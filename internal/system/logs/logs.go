// Package logs provides /var/log file browsing and systemd journal streaming.
package logs

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// LogFile represents a file or directory entry under /var/log.
type LogFile struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
}

// Unit represents a systemd service unit.
type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      string `json:"active"` // "active", "inactive", "failed", etc.
}

const logRoot = "/var/log"

// safePath ensures path is under /var/log and has no symlink escapes.
func safePath(path string) (string, error) {
	if path == "" {
		path = logRoot
	}
	// Clean and absolutize
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(logRoot, clean)
	}
	// Resolve symlinks so we catch escape attempts
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		// Path may not exist yet — check the cleaned form
		if !strings.HasPrefix(clean, logRoot+string(filepath.Separator)) && clean != logRoot {
			return "", os.ErrPermission
		}
		return clean, nil
	}
	if resolved != logRoot && !strings.HasPrefix(resolved, logRoot+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	return resolved, nil
}

// ListDir returns the immediate children of dir (non-recursive).
// Callers can walk sub-directories lazily by calling ListDir again.
func ListDir(dir string) ([]LogFile, error) {
	safe, err := safePath(dir)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(safe)
	if err != nil {
		return nil, err
	}

	files := make([]LogFile, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		absPath := filepath.Join(safe, e.Name())
		files = append(files, LogFile{
			Name:    e.Name(),
			Path:    absPath,
			Size:    info.Size(),
			ModTime: info.ModTime(),
			IsDir:   e.IsDir(),
		})
	}

	// Dirs first, then files; each group sorted by name
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}

// ReadTail returns the last `lines` lines of a log file.
func ReadTail(path string, lines int) ([]string, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = 500
	}
	if lines > 5000 {
		lines = 5000
	}

	f, err := os.Open(safe)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Read all lines into a ring buffer of size `lines`
	ring := make([]string, lines)
	pos := 0
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		ring[pos%lines] = scanner.Text()
		pos++
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if count <= lines {
		return ring[:count], nil
	}
	// Unwrap the ring
	out := make([]string, lines)
	start := pos % lines
	for i := 0; i < lines; i++ {
		out[i] = ring[(start+i)%lines]
	}
	return out, nil
}

// FollowFile tails path and writes new lines to w until ctx is cancelled.
// Each line is written as a complete string followed by a newline.
// Uses `tail -n 50 -F` so it handles log rotation.
func FollowFile(ctx context.Context, path string, w io.Writer) error {
	safe, err := safePath(path)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "tail", "-n", "50", "-F", safe)
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// unitRe validates a systemd unit name: letters, digits, hyphens, underscores, dots, @, :
var unitRe = regexp.MustCompile(`^[a-zA-Z0-9@:_\-\.]{1,256}$`)

// ListUnits returns all systemd service units. Returns nil if systemd is not available.
func ListUnits() ([]Unit, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil, nil
	}
	out, err := exec.Command(
		"systemctl", "list-units",
		"--type=service",
		"--all",
		"--no-pager",
		"--no-legend",
		"--plain",
	).Output()
	if err != nil {
		return nil, nil
	}

	var units []Unit
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: "  unit.service  loaded active running  Description text"
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name := strings.TrimPrefix(fields[0], "●")
		name = strings.TrimSpace(name)
		if !strings.HasSuffix(name, ".service") {
			continue
		}
		active := fields[2] // "active" | "inactive" | "failed" | ...
		desc := ""
		if len(fields) >= 5 {
			desc = strings.Join(fields[4:], " ")
		}
		units = append(units, Unit{
			Name:        strings.TrimSuffix(name, ".service"),
			Description: desc,
			Active:      active,
		})
	}
	return units, nil
}

// FollowUnit streams journalctl output for a systemd unit to w until ctx is cancelled.
func FollowUnit(ctx context.Context, unit string, w io.Writer) error {
	if !unitRe.MatchString(unit) {
		return os.ErrInvalid
	}
	// Ensure it ends with .service if no extension provided
	fullUnit := unit
	if !strings.Contains(unit, ".") {
		fullUnit = unit + ".service"
	}

	if _, err := exec.LookPath("journalctl"); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx,
		"journalctl",
		"-f",
		"-u", fullUnit,
		"-n", "50",
		"--output=short-iso",
		"--no-pager",
	)
	cmd.Stdout = w
	cmd.Stderr = io.Discard
	return cmd.Run()
}
