// Package files provides safe filesystem operations for the Webux file manager.
// All paths are validated against a configurable root to prevent traversal attacks.
package files

import (
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
	Name        string    `json:"name"`
	Path        string    `json:"path"`        // absolute path
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	Mode        string    `json:"mode"`        // e.g. "rwxr-xr-x"
	ModeOctal   string    `json:"mode_octal"`  // e.g. "0755"
	Owner       string    `json:"owner"`
	Group       string    `json:"group"`
	ModTime     time.Time `json:"mod_time"`
	MimeType    string    `json:"mime_type"`
	IsSymlink   bool      `json:"is_symlink"`
	SymlinkTarget string  `json:"symlink_target,omitempty"`
}

// Manager handles file operations with path safety enforcement.
type Manager struct {
	// Root restricts operations. Empty string = unrestricted (root only).
	Root string
}

// NewManager creates a Manager.
func NewManager(root string) *Manager {
	return &Manager{Root: root}
}

// safePath resolves and validates that path stays within Root.
func (m *Manager) safePath(path string) (string, error) {
	if path == "" {
		path = "/"
	}
	// Clean and make absolute
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		clean = "/" + clean
	}
	// If Root is set, enforce containment
	if m.Root != "" {
		root := filepath.Clean(m.Root)
		if !strings.HasPrefix(clean, root) {
			return "", fmt.Errorf("access denied: path is outside allowed root %s", root)
		}
	}
	return clean, nil
}

// List returns directory contents sorted: dirs first, then files.
func (m *Manager) List(path string) ([]Entry, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(safe)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", safe, err)
	}

	var out []Entry
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := entryFromInfo(safe, e.Name(), info)
		// Resolve symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			entry.IsSymlink = true
			if target, err := os.Readlink(filepath.Join(safe, e.Name())); err == nil {
				entry.SymlinkTarget = target
			}
		}
		out = append(out, entry)
	}

	// Sort: directories first, then alphabetical
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Read returns file contents. Refuses to read files over 10MB.
func (m *Manager) Read(path string) ([]byte, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return nil, err
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

// Write writes content to a file. Returns CLI equivalent.
func (m *Manager) Write(path, content string) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(safe, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write %s: %w", safe, err)
	}
	return fmt.Sprintf("# Content written to %s", safe), nil
}

// Delete removes a file or empty directory. Returns CLI equivalent.
func (m *Manager) Delete(path string) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
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

// Rename moves or renames a file. Returns CLI equivalent.
func (m *Manager) Rename(oldPath, newPath string) (string, error) {
	safeOld, err := m.safePath(oldPath)
	if err != nil {
		return "", err
	}
	safeNew, err := m.safePath(newPath)
	if err != nil {
		return "", err
	}
	if err := os.Rename(safeOld, safeNew); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	return fmt.Sprintf("mv %s %s", safeOld, safeNew), nil
}

// Mkdir creates a directory (and parents). Returns CLI equivalent.
func (m *Manager) Mkdir(path string) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(safe, 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	return "mkdir -p " + safe, nil
}

// Chmod changes file permissions. mode is an octal string e.g. "755".
func (m *Manager) Chmod(path, mode string) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
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

// Chown changes file ownership. Returns CLI equivalent.
func (m *Manager) Chown(path, owner, group string) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
	}
	ownerGroup := owner
	if group != "" {
		ownerGroup += ":" + group
	}
	if err := exec.Command("chown", ownerGroup, safe).Run(); err != nil {
		return "", fmt.Errorf("chown: %w", err)
	}
	return fmt.Sprintf("chown %s %s", ownerGroup, safe), nil
}

// Upload writes uploaded data to a path. Returns CLI equivalent.
func (m *Manager) Upload(path string, r io.Reader) (string, error) {
	safe, err := m.safePath(path)
	if err != nil {
		return "", err
	}
	// Ensure parent directory exists
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

// Stat returns a single entry's metadata.
func (m *Manager) Stat(path string) (*Entry, error) {
	safe, err := m.safePath(path)
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

// --- helpers ----------------------------------------------------------------

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

	// Get owner/group via stat -c
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
