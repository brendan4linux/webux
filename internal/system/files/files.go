// Package files provides filesystem operations for the Webux file manager.
// In normal mode operations run as the webux process user.
// In sudo mode, operations are wrapped in "sudo -n" for root access.
package files

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is a file or directory listing entry.
type Entry struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	IsDir         bool      `json:"is_dir"`
	Size          int64     `json:"size"`
	Mode          string    `json:"mode"`
	ModeOctal     string    `json:"mode_octal"`
	Owner         string    `json:"owner"`
	Group         string    `json:"group"`
	ModTime       time.Time `json:"mod_time"`
	MimeType      string    `json:"mime_type"`
	IsSymlink     bool      `json:"is_symlink"`
	SymlinkTarget string    `json:"symlink_target,omitempty"`
}

// Manager handles file operations.
// Sudo=true wraps every operation in "sudo -n" (requires NOPASSWD sudo or root process).
type Manager struct {
	Sudo bool
}

// NewManager creates a Manager. sudo=true enables root escalation via sudo.
func NewManager(sudo bool) *Manager {
	return &Manager{Sudo: sudo}
}

// safePath cleans and absolutizes a path. No root restriction — the OS enforces permissions.
func safePath(path string) (string, error) {
	if path == "" {
		path = "/"
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		clean = "/" + clean
	}
	return clean, nil
}

// ── List ─────────────────────────────────────────────────────────────────────

// List returns directory contents sorted: dirs first, then files.
func (m *Manager) List(path string) ([]Entry, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	if m.Sudo {
		return m.sudoList(safe)
	}
	return nativeList(safe)
}

func nativeList(path string) ([]Entry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", path, err)
	}
	var out []Entry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := entryFromInfo(path, e.Name(), info)
		if info.Mode()&os.ModeSymlink != 0 {
			entry.IsSymlink = true
			if target, err := os.Readlink(filepath.Join(path, e.Name())); err == nil {
				entry.SymlinkTarget = target
			}
		}
		out = append(out, entry)
	}
	return sortEntries(out), nil
}

// sudoList uses GNU find's -printf to list a directory with elevated privileges.
// Format per line: name\ttype\tsize\toctal_mode\towner\tgroup\tunix_ts\tsymlink_target
func (m *Manager) sudoList(path string) ([]Entry, error) {
	out, err := exec.Command("sudo", "-n", "find", path,
		"-maxdepth", "1", "-mindepth", "1",
		"-printf", "%f\t%y\t%s\t%#m\t%U\t%G\t%T@\t%l\n").Output()
	if err != nil {
		return nil, fmt.Errorf("sudo list %s: %w", path, err)
	}
	var entries []Entry
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 8)
		if len(parts) < 7 {
			continue
		}
		name, ftype, sizeStr, octal, owner, group, tsStr := parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
		symTarget := ""
		if len(parts) == 8 {
			symTarget = parts[7]
		}
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		tsF, _ := strconv.ParseFloat(tsStr, 64)
		modTime := time.Unix(int64(tsF), 0)
		isDir := ftype == "d"
		isSymlink := ftype == "l"
		fullPath := filepath.Join(path, name)
		mimeType := ""
		if !isDir {
			mimeType = mime.TypeByExtension(filepath.Ext(name))
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}
		}
		entries = append(entries, Entry{
			Name:          name,
			Path:          fullPath,
			IsDir:         isDir,
			Size:          size,
			Mode:          "",
			ModeOctal:     "0" + octal,
			Owner:         owner,
			Group:         group,
			ModTime:       modTime,
			MimeType:      mimeType,
			IsSymlink:     isSymlink,
			SymlinkTarget: symTarget,
		})
	}
	return sortEntries(entries), nil
}

// ── Read ──────────────────────────────────────────────────────────────────────

func (m *Manager) Read(path string) ([]byte, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	if m.Sudo {
		return exec.Command("sudo", "-n", "cat", safe).Output()
	}
	info, err := os.Stat(safe)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > 10*1024*1024 {
		return nil, fmt.Errorf("file too large to read in browser (max 10MB)")
	}
	return os.ReadFile(safe)
}

// ── Write ─────────────────────────────────────────────────────────────────────

func (m *Manager) Write(path, content string) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		cmd := exec.Command("sudo", "-n", "tee", safe)
		cmd.Stdin = strings.NewReader(content)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("sudo write %s: %s", safe, stderr.String())
		}
		return fmt.Sprintf("sudo tee %s", safe), nil
	}
	if err := os.WriteFile(safe, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", safe, err)
	}
	return fmt.Sprintf("# Content written to %s", safe), nil
}

// ── Delete ───────────────────────────────────────────────────────────────────

func (m *Manager) Delete(path string) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		if out, err := exec.Command("sudo", "-n", "rm", "-rf", safe).CombinedOutput(); err != nil {
			return "", fmt.Errorf("sudo rm %s: %s", safe, string(out))
		}
		return "sudo rm -rf " + safe, nil
	}
	info, err := os.Stat(safe)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if err := os.Remove(safe); err != nil {
			return "", fmt.Errorf("remove dir (must be empty): %w", err)
		}
		return "rmdir " + safe, nil
	}
	if err := os.Remove(safe); err != nil {
		return "", fmt.Errorf("remove: %w", err)
	}
	return "rm " + safe, nil
}

// ── Rename ────────────────────────────────────────────────────────────────────

func (m *Manager) Rename(oldPath, newPath string) (string, error) {
	safeOld, err := safePath(oldPath)
	if err != nil {
		return "", err
	}
	safeNew, err := safePath(newPath)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		if out, err := exec.Command("sudo", "-n", "mv", safeOld, safeNew).CombinedOutput(); err != nil {
			return "", fmt.Errorf("sudo mv: %s", string(out))
		}
		return fmt.Sprintf("sudo mv %s %s", safeOld, safeNew), nil
	}
	if err := os.Rename(safeOld, safeNew); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	return fmt.Sprintf("mv %s %s", safeOld, safeNew), nil
}

// ── Mkdir ─────────────────────────────────────────────────────────────────────

func (m *Manager) Mkdir(path string) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		if out, err := exec.Command("sudo", "-n", "mkdir", "-p", safe).CombinedOutput(); err != nil {
			return "", fmt.Errorf("sudo mkdir: %s", string(out))
		}
		return "sudo mkdir -p " + safe, nil
	}
	if err := os.MkdirAll(safe, 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	return "mkdir -p " + safe, nil
}

// ── Chmod ─────────────────────────────────────────────────────────────────────

func (m *Manager) Chmod(path, mode string) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		if out, err := exec.Command("sudo", "-n", "chmod", mode, safe).CombinedOutput(); err != nil {
			return "", fmt.Errorf("sudo chmod: %s", string(out))
		}
		return fmt.Sprintf("sudo chmod %s %s", mode, safe), nil
	}
	val, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return "", fmt.Errorf("invalid mode %q: %w", mode, err)
	}
	if err := os.Chmod(safe, fs.FileMode(val)); err != nil {
		return "", fmt.Errorf("chmod: %w", err)
	}
	return fmt.Sprintf("chmod %s %s", mode, safe), nil
}

// ── Chown ─────────────────────────────────────────────────────────────────────

func (m *Manager) Chown(path, owner, group string) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	ownerGroup := owner
	if group != "" {
		ownerGroup += ":" + group
	}
	args := []string{}
	if m.Sudo {
		args = append(args, "sudo", "-n")
	}
	args = append(args, "chown", ownerGroup, safe)
	if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
		return "", fmt.Errorf("chown: %w", err)
	}
	prefix := ""
	if m.Sudo {
		prefix = "sudo "
	}
	return fmt.Sprintf("%schown %s %s", prefix, ownerGroup, safe), nil
}

// ── Upload ────────────────────────────────────────────────────────────────────

func (m *Manager) Upload(path string, r io.Reader) (string, error) {
	safe, err := safePath(path)
	if err != nil {
		return "", err
	}
	if m.Sudo {
		// Write to a temp file, then sudo mv it into place
		tmp, err := os.CreateTemp("", "webux-upload-*")
		if err != nil {
			return "", fmt.Errorf("temp file: %w", err)
		}
		tmpName := tmp.Name()
		defer os.Remove(tmpName)
		if _, err := io.Copy(tmp, r); err != nil {
			tmp.Close()
			return "", fmt.Errorf("write temp: %w", err)
		}
		tmp.Close()
		if out, err := exec.Command("sudo", "-n", "mv", tmpName, safe).CombinedOutput(); err != nil {
			return "", fmt.Errorf("sudo mv upload: %s", string(out))
		}
		return fmt.Sprintf("# File uploaded to %s (via sudo)", safe), nil
	}
	if err := os.MkdirAll(filepath.Dir(safe), 0755); err != nil {
		return "", fmt.Errorf("mkdir parent: %w", err)
	}
	f, err := os.Create(safe)
	if err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write upload: %w", err)
	}
	return fmt.Sprintf("# File uploaded to %s", safe), nil
}

// ── Stat ──────────────────────────────────────────────────────────────────────

func (m *Manager) Stat(path string) (*Entry, error) {
	safe, err := safePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(safe)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(safe)
	e := entryFromInfo(dir, info.Name(), info)
	return &e, nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func sortEntries(out []Entry) []Entry {
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func entryFromInfo(dir, name string, info os.FileInfo) Entry {
	path := filepath.Join(dir, name)
	mode := info.Mode()
	mimeType := ""
	if !info.IsDir() {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
	}
	owner, group := fileOwner(path)
	return Entry{
		Name:      name,
		Path:      path,
		IsDir:     info.IsDir(),
		Size:      info.Size(),
		Mode:      mode.String(),
		ModeOctal: fmt.Sprintf("%04o", mode.Perm()),
		Owner:     owner,
		Group:     group,
		ModTime:   info.ModTime(),
		MimeType:  mimeType,
	}
}

func fileOwner(path string) (owner, group string) {
	out, err := exec.Command("stat", "-c", "%U %G", path).Output()
	if err != nil {
		return "", ""
	}
	parts := strings.Fields(strings.TrimSpace(string(out)))
	if len(parts) >= 2 {
		return parts[0], parts[1]
	}
	return "", ""
}
