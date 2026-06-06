package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/cron"
	"github.com/brendan4linux/webux/internal/system/files"
	"github.com/brendan4linux/webux/internal/system/webservers"
)

// ─── Webservers ─────────────────────────────────────────────────────────────

type WebserversHandler struct {
	mgr   *webservers.Manager
	learn *learn.Store
}

func NewWebserversHandler(ls *learn.Store) *WebserversHandler {
	return &WebserversHandler{mgr: webservers.NewManager(), learn: ls}
}

// List handles GET /api/webservers
func (h *WebserversHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "webservers")
	servers := h.mgr.List()
	h.learn.Echo(ctx,
		"nginx -v 2>&1; apache2 -v 2>&1; caddy version",
		"Detects running webservers by checking binaries and config paths")
	writeJSON(w, map[string]interface{}{"servers": servers, "count": len(servers)})
}

// GetConfig handles GET /api/webservers/{type}/config
func (h *WebserversHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	ctx := learn.WithContext(r.Context(), "webservers")
	content, path, err := h.mgr.ReadConfig(wsType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.learn.Echo(ctx, "cat "+path, "Reads the "+string(wsType)+" configuration file")
	writeJSON(w, map[string]interface{}{"content": content, "path": path})
}

// SaveConfig handles PUT /api/webservers/{type}/config
func (h *WebserversHandler) SaveConfig(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	ctx := learn.WithContext(r.Context(), "webservers")
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	cliCmd, err := h.mgr.WriteConfig(wsType, body.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Saves updated "+string(wsType)+" config (tested before writing)")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// TestConfig handles POST /api/webservers/{type}/test
func (h *WebserversHandler) TestConfig(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	ctx := learn.WithContext(r.Context(), "webservers")
	cliCmd, err := h.mgr.TestConfig(wsType)
	if err != nil {
		h.learn.Echo(ctx, cliCmd, "Tests the "+string(wsType)+" config syntax")
		writeJSON(w, map[string]interface{}{"ok": false, "error": err.Error(), "cli_cmd": cliCmd})
		return
	}
	h.learn.Echo(ctx, cliCmd, "Config syntax OK for "+string(wsType))
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Reload handles POST /api/webservers/{type}/reload
func (h *WebserversHandler) Reload(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	ctx := learn.WithContext(r.Context(), "webservers")
	cliCmd, err := h.mgr.Reload(wsType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Reloads "+string(wsType)+" config without dropping connections")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// ListSites handles GET /api/webservers/{type}/sites
func (h *WebserversHandler) ListSites(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	ctx := learn.WithContext(r.Context(), "webservers")
	sites, err := h.mgr.ListSites(wsType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.learn.Echo(ctx, "ls /etc/"+string(wsType)+"/sites-enabled/", "Lists enabled virtual hosts")
	writeJSON(w, map[string]interface{}{"sites": sites})
}

// EnableSite handles POST /api/webservers/{type}/sites/{site}/enable
func (h *WebserversHandler) EnableSite(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	site := chi.URLParam(r, "site")
	ctx := learn.WithContext(r.Context(), "webservers")
	cliCmd, err := h.mgr.EnableSite(wsType, site)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Enables the "+site+" virtual host")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// DisableSite handles POST /api/webservers/{type}/sites/{site}/disable
func (h *WebserversHandler) DisableSite(w http.ResponseWriter, r *http.Request) {
	wsType := webservers.Type(chi.URLParam(r, "type"))
	site := chi.URLParam(r, "site")
	ctx := learn.WithContext(r.Context(), "webservers")
	cliCmd, err := h.mgr.DisableSite(wsType, site)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Disables the "+site+" virtual host")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// ─── Files ──────────────────────────────────────────────────────────────────

type FilesHandler struct {
	mgr   *files.Manager
	learn *learn.Store
}

func NewFilesHandler(ls *learn.Store) *FilesHandler {
	return &FilesHandler{mgr: files.NewManager(""), learn: ls}
}

// List handles GET /api/files?path=/etc
func (h *FilesHandler) List(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	ctx := learn.WithContext(r.Context(), "files")
	entries, err := h.mgr.List(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.learn.Echo(ctx, "ls -la "+path, "Lists directory contents with permissions and ownership")
	writeJSON(w, map[string]interface{}{"entries": entries, "path": path})
}

// Read handles GET /api/files/read?path=/etc/nginx/nginx.conf
func (h *FilesHandler) Read(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	ctx := learn.WithContext(r.Context(), "files")
	data, err := h.mgr.Read(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.learn.Echo(ctx, "cat "+path, "Reads the file at "+path)
	writeJSON(w, map[string]interface{}{"content": string(data), "path": path})
}

// Write handles PUT /api/files/write
func (h *FilesHandler) Write(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Write(body.Path, body.Content)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Writes content to "+body.Path)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Delete handles DELETE /api/files?path=/tmp/foo
func (h *FilesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Delete(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes "+path)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Mkdir handles POST /api/files/mkdir
func (h *FilesHandler) Mkdir(w http.ResponseWriter, r *http.Request) {
	var body struct{ Path string `json:"path"` }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Mkdir(body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Creates directory "+body.Path)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Rename handles POST /api/files/rename
func (h *FilesHandler) Rename(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OldPath string `json:"old_path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Rename(body.OldPath, body.NewPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Renames/moves "+body.OldPath+" to "+body.NewPath)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Chmod handles POST /api/files/chmod
func (h *FilesHandler) Chmod(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Chmod(body.Path, body.Mode)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Changes permissions of "+body.Path+" to "+body.Mode)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Upload handles POST /api/files/upload?path=/tmp/
func (h *FilesHandler) Upload(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("path")
	if dir == "" {
		dir = "/tmp"
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50MB limit
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Sanitise filename
	safeName := filepath.Base(header.Filename)
	safeName = strings.ReplaceAll(safeName, "..", "")
	dest := filepath.Join(dir, safeName)

	ctx := learn.WithContext(r.Context(), "files")
	cliCmd, err := h.mgr.Upload(dest, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Uploaded "+safeName+" to "+dir)
	writeJSON(w, map[string]interface{}{"ok": true, "path": dest, "cli_cmd": cliCmd})
}

// Download handles GET /api/files/download?path=/etc/hosts
func (h *FilesHandler) Download(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	data, err := h.mgr.Read(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := filepath.Base(path)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

// ─── Cron ───────────────────────────────────────────────────────────────────

type CronHandler struct {
	mgr   *cron.Manager
	learn *learn.Store
}

func NewCronHandler(ls *learn.Store) *CronHandler {
	return &CronHandler{mgr: cron.NewManager(), learn: ls}
}

// List handles GET /api/cron
func (h *CronHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "cron")
	jobs, err := h.mgr.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx,
		"crontab -l && cat /etc/crontab && ls /etc/cron.d/",
		"Reads all cron jobs from /etc/crontab, /etc/cron.d/, and user spool directories")
	writeJSON(w, map[string]interface{}{"jobs": jobs, "count": len(jobs)})
}

// Schedules handles GET /api/cron/schedules — returns preset options for the UI
func (h *CronHandler) Schedules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"schedules": cron.CommonSchedules()})
}

// Add handles POST /api/cron
func (h *CronHandler) Add(w http.ResponseWriter, r *http.Request) {
	var job cron.Job
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := cron.ValidateSchedule(job.Schedule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "cron")
	cliCmd, err := h.mgr.Add(job)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Adds cron job: "+job.Schedule+" "+job.Command)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Delete handles DELETE /api/cron
func (h *CronHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var job cron.Job
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "cron")
	cliCmd, err := h.mgr.Delete(job)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Removes cron job from "+job.SourceFile)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Update handles PUT /api/cron
// Body: {"old": <CronJob>, "updated": <CronJob>}
func (h *CronHandler) Update(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Old     cron.Job `json:"old"`
		Updated cron.Job `json:"updated"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if err := cron.ValidateSchedule(body.Updated.Schedule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := learn.WithContext(r.Context(), "cron")
	cliCmd, err := h.mgr.Update(body.Old, body.Updated)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Updates cron job at line "+itoa(body.Old.LineNumber)+" in "+body.Old.SourceFile)
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}


func (h *CronHandler) Validate(w http.ResponseWriter, r *http.Request) {
	var body struct{ Schedule string `json:"schedule"` }
	json.NewDecoder(r.Body).Decode(&body)
	err := cron.ValidateSchedule(body.Schedule)
	if err != nil {
		writeJSON(w, map[string]interface{}{"valid": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"valid": true})
}

// ensure io is used
var _ io.Reader = nil
