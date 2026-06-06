package handlers

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/ansible"
)

type AnsibleHandler struct {
	scanner *ansible.Scanner
	runner  *ansible.Runner
	learn   *learn.Store
	db      *sql.DB
}

func NewAnsibleHandler(ls *learn.Store, db *sql.DB) *AnsibleHandler {
	return &AnsibleHandler{
		scanner: ansible.NewScanner(),
		runner:  ansible.NewRunner(),
		learn:   ls,
		db:      db,
	}
}

func (h *AnsibleHandler) getSetting(key, def string) string {
	var val string
	if err := h.db.QueryRow("SELECT value FROM webux_settings WHERE key = ?", key).Scan(&val); err != nil || val == "" {
		return def
	}
	return val
}

func (h *AnsibleHandler) setSetting(key, val string) {
	h.db.Exec(`INSERT INTO webux_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`, key, val)
}

// Status handles GET /api/ansible/status
func (h *AnsibleHandler) Status(w http.ResponseWriter, r *http.Request) {
	installed := h.runner.Installed()
	version := ""
	if installed {
		version = h.runner.Version()
	}
	writeJSON(w, map[string]interface{}{
		"installed":    installed,
		"version":      version,
		"playbook_dir": h.getSetting("ansible.playbook_dir", "/etc/ansible"),
		"inventory":    h.getSetting("ansible.inventory", "/etc/ansible/hosts"),
	})
}

// Settings handles PUT /api/ansible/settings
func (h *AnsibleHandler) Settings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PlaybookDir string `json:"playbook_dir"`
		Inventory   string `json:"inventory"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.PlaybookDir != "" {
		h.setSetting("ansible.playbook_dir", body.PlaybookDir)
	}
	if body.Inventory != "" {
		h.setSetting("ansible.inventory", body.Inventory)
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// ListPlaybooks handles GET /api/ansible/playbooks
func (h *AnsibleHandler) ListPlaybooks(w http.ResponseWriter, r *http.Request) {
	dir := h.getSetting("ansible.playbook_dir", "/etc/ansible")
	ctx := learn.WithContext(r.Context(), "ansible")

	playbooks, err := h.scanner.Scan(dir)
	if err != nil {
		writeJSON(w, map[string]interface{}{
			"playbooks": []interface{}{},
			"error":     err.Error(),
			"dir":       dir,
		})
		return
	}

	h.learn.Echo(ctx,
		fmt.Sprintf("find %s -maxdepth 1 -name '*.yml' | xargs grep -l 'hosts:'", dir),
		"Scans "+dir+" for Ansible playbooks and parses their declared variables")

	writeJSON(w, map[string]interface{}{
		"playbooks": playbooks,
		"count":     len(playbooks),
		"dir":       dir,
	})
}

// ListInventory handles GET /api/ansible/inventory
func (h *AnsibleHandler) ListInventory(w http.ResponseWriter, r *http.Request) {
	inv := h.getSetting("ansible.inventory", "/etc/ansible/hosts")
	ctx := learn.WithContext(r.Context(), "ansible")
	groups, err := ansible.ParseInventory(inv)
	if err != nil {
		writeJSON(w, map[string]interface{}{"groups": []interface{}{}, "error": err.Error()})
		return
	}
	h.learn.Echo(ctx, "ansible-inventory --list -i "+inv,
		"Parses the Ansible inventory file for host groups")
	writeJSON(w, map[string]interface{}{"groups": groups, "inventory": inv})
}

// Run handles POST /api/ansible/run — streams SSE output
func (h *AnsibleHandler) Run(w http.ResponseWriter, r *http.Request) {
	var opts ansible.RunOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if opts.Inventory == "" {
		opts.Inventory = h.getSetting("ansible.inventory", "/etc/ansible/hosts")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "ansible")
	out := make(chan string, 64)

	go func() {
		cliCmd, err := h.runner.Run(r.Context(), opts, out)
		if err != nil {
			out <- fmt.Sprintf("[webux] error: %s", err.Error())
		}
		h.learn.Echo(ctx, cliCmd, "Runs Ansible playbook: "+opts.PlaybookPath)
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// Install handles POST /api/ansible/install — installs ansible via native package manager
func (h *AnsibleHandler) Install(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	out := make(chan string, 32)
	go func() {
		installAnsible(r.Context(), out)
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// installAnsible detects the package manager and installs ansible.
func installAnsible(ctx context.Context, out chan<- string) {
	var args []string
	switch {
	case commandExists("pacman"):
		args = []string{"pacman", "-S", "--noconfirm", "ansible"}
	case commandExists("apt-get"):
		args = []string{"apt-get", "install", "-y", "ansible"}
	case commandExists("dnf"):
		args = []string{"dnf", "install", "-y", "ansible"}
	case commandExists("yum"):
		args = []string{"yum", "install", "-y", "ansible"}
	default:
		out <- "[webux] No supported package manager found"
		return
	}

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		out <- "[webux] Failed to start install: " + err.Error()
		return
	}
	scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
	for scanner.Scan() {
		select {
		case out <- scanner.Text():
		case <-ctx.Done():
			cmd.Process.Kill()
			return
		}
	}
	cmd.Wait()
}
