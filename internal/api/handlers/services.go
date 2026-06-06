package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/initsys"
)

// ServicesHandler manages init system units via the InitSystem interface.
type ServicesHandler struct {
	init  initsys.InitSystem
	learn *learn.Store
}

func NewServicesHandler(is initsys.InitSystem, ls *learn.Store) *ServicesHandler {
	return &ServicesHandler{init: is, learn: ls}
}

// List handles GET /api/services?type=service
func (h *ServicesHandler) List(w http.ResponseWriter, r *http.Request) {
	if h.init == nil {
		writeJSON(w, map[string]interface{}{
			"units":       []interface{}{},
			"init_system": "unknown",
			"error":       "no supported init system detected",
		})
		return
	}

	unitType := r.URL.Query().Get("type") // "" = all, "service", "socket", "timer"

	ctx := learn.WithContext(r.Context(), "services")
	units, err := h.init.List(ctx, unitType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cliCmd := "systemctl list-units --all --no-pager"
	if unitType != "" {
		cliCmd = "systemctl list-units --type=" + unitType + " --all --no-pager"
	}
	h.learn.Echo(ctx, cliCmd, "Lists all "+orAll(unitType)+" units known to "+h.init.Name())

	writeJSON(w, map[string]interface{}{
		"units":       units,
		"init_system": h.init.Name(),
		"count":       len(units),
	})
}

// Get handles GET /api/services/{name}
func (h *ServicesHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.init == nil {
		http.Error(w, "no init system", http.StatusServiceUnavailable)
		return
	}
	ctx := learn.WithContext(r.Context(), "services")
	unit, err := h.init.Get(ctx, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.learn.Echo(ctx,
		"systemctl status "+name,
		"Shows the current status of the "+name+" unit")
	writeJSON(w, unit)
}

// Action handles POST /api/services/{name}/action
// Body: {"action": "start"|"stop"|"restart"|"reload"|"enable"|"disable"}
func (h *ServicesHandler) Action(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.init == nil {
		http.Error(w, "no init system", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := learn.WithContext(r.Context(), "services")
	var cliCmd string
	var err error

	switch strings.ToLower(body.Action) {
	case "start":
		cliCmd, err = h.init.Start(ctx, name)
	case "stop":
		cliCmd, err = h.init.Stop(ctx, name)
	case "restart":
		cliCmd, err = h.init.Restart(ctx, name)
	case "reload":
		cliCmd, err = h.init.Reload(ctx, name)
	case "enable":
		cliCmd, err = h.init.Enable(ctx, name)
	case "disable":
		cliCmd, err = h.init.Disable(ctx, name)
	default:
		http.Error(w, "unknown action: "+body.Action, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd,
		strings.Title(body.Action)+"s the "+name+" unit via "+h.init.Name())

	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"action":  body.Action,
		"unit":    name,
		"cli_cmd": cliCmd,
	})
}

// Logs handles GET /api/services/{name}/logs?lines=100
func (h *ServicesHandler) Logs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if h.init == nil {
		http.Error(w, "no init system", http.StatusServiceUnavailable)
		return
	}

	lines := 100
	if l := r.URL.Query().Get("lines"); l != "" {
		if n, err := parseInt(l); err == nil && n > 0 {
			lines = n
		}
	}

	ctx := learn.WithContext(r.Context(), "services")
	entries, err := h.init.Logs(ctx, name, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx,
		"journalctl -u "+name+" -n "+itoa(lines)+" --no-pager",
		"Fetches the last "+itoa(lines)+" log lines for "+name)

	writeJSON(w, map[string]interface{}{
		"unit":    name,
		"entries": entries,
		"count":   len(entries),
	})
}

func orAll(s string) string {
	if s == "" {
		return "all"
	}
	return s
}
