package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/packages"
)

type PackagesHandler struct {
	mgr   *packages.Manager
	learn *learn.Store
}

func NewPackagesHandler(ls *learn.Store) *PackagesHandler {
	return &PackagesHandler{
		mgr:   packages.NewManager(),
		learn: ls,
	}
}

// Info handles GET /api/packages/info — returns detected package manager
func (h *PackagesHandler) Info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"family":      h.mgr.Family,
		"backend":     h.mgr.Backend,
		"has_flatpak": packages.HasFlatpak(),
	})
}

// ListInstalled handles GET /api/packages
func (h *PackagesHandler) ListInstalled(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "packages")
	pkgs, err := h.mgr.ListInstalled()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cliCmd string
	switch h.mgr.Family {
	case packages.FamilyPacman:
		cliCmd = "pacman -Q"
	case packages.FamilyApt:
		cliCmd = "dpkg-query -W"
	default:
		cliCmd = "rpm -qa"
	}
	h.learn.Echo(ctx, cliCmd, "Lists all installed packages")
	writeJSON(w, map[string]interface{}{"packages": pkgs, "count": len(pkgs), "family": h.mgr.Family})
}

// ListUpgradable handles GET /api/packages/upgradable
func (h *PackagesHandler) ListUpgradable(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "packages")
	pkgs, err := h.mgr.ListUpgradable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cliCmd string
	switch h.mgr.Family {
	case packages.FamilyPacman:
		cliCmd = "pacman -Qu"
	case packages.FamilyApt:
		cliCmd = "apt-get -s upgrade"
	default:
		cliCmd = fmt.Sprintf("%s check-update", h.mgr.Backend)
	}
	h.learn.Echo(ctx, cliCmd, "Checks for available package upgrades")
	writeJSON(w, map[string]interface{}{"packages": pkgs, "count": len(pkgs)})
}

// Search handles GET /api/packages/search?q=nginx
func (h *PackagesHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "q parameter required", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	pkgs, cliCmd, err := h.mgr.Search(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Searches available packages for "+query)
	writeJSON(w, map[string]interface{}{"packages": pkgs, "count": len(pkgs), "query": query})
}

// Install handles POST /api/packages/install — streams SSE
func (h *PackagesHandler) Install(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	h.streamPackageOp(w, r, body.Name, "install")
}

// Remove handles POST /api/packages/remove — streams SSE
func (h *PackagesHandler) Remove(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	h.streamPackageOp(w, r, body.Name, "remove")
}

// Upgrade handles POST /api/packages/upgrade — streams SSE
// Body: {"name": "nginx"} or {} for full system upgrade
func (h *PackagesHandler) Upgrade(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&body)
	h.streamPackageOp(w, r, body.Name, "upgrade")
}

// UpdateCache handles POST /api/packages/update-cache — streams SSE
func (h *PackagesHandler) UpdateCache(w http.ResponseWriter, r *http.Request) {
	h.streamPackageOp(w, r, "", "update-cache")
}

func (h *PackagesHandler) streamPackageOp(w http.ResponseWriter, r *http.Request, name, op string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "packages")
	out := make(chan string, 64)

	go func() {
		var cliCmd string
		var err error
		switch op {
		case "install":
			cliCmd, err = h.mgr.Install(r.Context(), name, out)
		case "remove":
			cliCmd, err = h.mgr.Remove(r.Context(), name, out)
		case "upgrade":
			cliCmd, err = h.mgr.Upgrade(r.Context(), name, out)
		case "update-cache":
			cliCmd, err = h.mgr.UpdateCache(r.Context(), out)
		}
		if err != nil {
			out <- fmt.Sprintf("[webux] error: %s", err.Error())
		}
		if cliCmd != "" {
			h.learn.Echo(ctx, cliCmd, fmt.Sprintf("Package operation: %s %s", op, name))
		}
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// ── Repos ─────────────────────────────────────────────────────────────────

// ListRepos handles GET /api/packages/repos
func (h *PackagesHandler) ListRepos(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "packages")
	repos, err := h.mgr.ListRepos()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var cliCmd string
	switch h.mgr.Family {
	case packages.FamilyPacman:
		cliCmd = "grep -E '\\[.+\\]' /etc/pacman.conf"
	case packages.FamilyApt:
		cliCmd = "cat /etc/apt/sources.list /etc/apt/sources.list.d/*"
	default:
		cliCmd = "ls /etc/yum.repos.d/"
	}
	h.learn.Echo(ctx, cliCmd, "Lists all configured package repositories")
	writeJSON(w, map[string]interface{}{"repos": repos, "count": len(repos)})
}

// EnableRepo handles POST /api/packages/repos/enable
func (h *PackagesHandler) EnableRepo(w http.ResponseWriter, r *http.Request) {
	var repo packages.Repo
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := h.mgr.EnableRepo(repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Enables repository "+repo.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// DisableRepo handles POST /api/packages/repos/disable
func (h *PackagesHandler) DisableRepo(w http.ResponseWriter, r *http.Request) {
	var repo packages.Repo
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := h.mgr.DisableRepo(repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Disables repository "+repo.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// AddRepo handles POST /api/packages/repos
func (h *PackagesHandler) AddRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		KeyURL string `json:"key_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		http.Error(w, "name and url are required", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := h.mgr.AddRepo(body.Name, body.URL, body.KeyURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Adds repository "+body.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// RemoveRepo handles DELETE /api/packages/repos
func (h *PackagesHandler) RemoveRepo(w http.ResponseWriter, r *http.Request) {
	var repo packages.Repo
	if err := json.NewDecoder(r.Body).Decode(&repo); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := h.mgr.RemoveRepo(repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes repository "+repo.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// ListFlatpakRemotes handles GET /api/packages/repos/flatpak
func (h *PackagesHandler) ListFlatpakRemotes(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "packages")
	remotes, err := packages.ListFlatpakRemotes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, "flatpak remotes --columns=name,url,disabled,type",
		"Lists all configured Flatpak remote repositories")
	writeJSON(w, map[string]interface{}{"remotes": remotes})
}

// AddFlatpakRemote handles POST /api/packages/repos/flatpak
func (h *PackagesHandler) AddFlatpakRemote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		System bool   `json:"system"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		http.Error(w, "name and url are required", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := packages.AddFlatpakRemote(body.Name, body.URL, body.System)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Adds Flatpak remote "+body.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// RemoveFlatpakRemote handles DELETE /api/packages/repos/flatpak
func (h *PackagesHandler) RemoveFlatpakRemote(w http.ResponseWriter, r *http.Request) {
	var body struct{ Name string `json:"name"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "packages")
	cliCmd, err := packages.RemoveFlatpakRemote(body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes Flatpak remote "+body.Name)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// ── Flatpak ──────────────────────────────────────────────────────────────

// ListFlatpaks handles GET /api/packages/flatpak
func (h *PackagesHandler) ListFlatpaks(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "packages")
	apps, err := packages.ListFlatpaks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, "flatpak list --columns=name,application,version,branch,origin,installation",
		"Lists all installed Flatpak applications")
	writeJSON(w, map[string]interface{}{"apps": apps, "count": len(apps)})
}

// RemoveFlatpak handles POST /api/packages/flatpak/remove — streams SSE
func (h *PackagesHandler) RemoveFlatpak(w http.ResponseWriter, r *http.Request) {
	var body struct{ AppID string `json:"app_id"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AppID == "" {
		http.Error(w, "app_id is required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "packages")
	out := make(chan string, 32)
	go func() {
		cliCmd, err := packages.RemoveFlatpak(r.Context(), body.AppID, out)
		if err != nil {
			out <- fmt.Sprintf("[webux] error: %s", err.Error())
		}
		h.learn.Echo(ctx, cliCmd, "Removes Flatpak app "+body.AppID)
		close(out)
	}()
	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// UpdateFlatpaks handles POST /api/packages/flatpak/update — streams SSE
func (h *PackagesHandler) UpdateFlatpaks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "packages")
	out := make(chan string, 32)
	go func() {
		cliCmd, err := packages.UpdateFlatpaks(r.Context(), out)
		if err != nil {
			out <- fmt.Sprintf("[webux] error: %s", err.Error())
		}
		h.learn.Echo(ctx, cliCmd, "Updates all Flatpak applications")
		close(out)
	}()
	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}
