package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/processes"
)

// ProcessesHandler serves live process data from /proc.
type ProcessesHandler struct {
	learn *learn.Store
}

func NewProcessesHandler(ls *learn.Store) *ProcessesHandler {
	return &ProcessesHandler{learn: ls}
}

// List handles GET /api/processes?sort=cpu&order=desc&filter=nginx
func (h *ProcessesHandler) List(w http.ResponseWriter, r *http.Request) {
	sortField := r.URL.Query().Get("sort")
	if sortField == "" {
		sortField = "cpu"
	}
	order := r.URL.Query().Get("order")
	asc := order == "asc"

	ctx := learn.WithContext(r.Context(), "processes")

	scanner := processes.NewScanner()
	procs, err := scanner.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	processes.SortBy(procs, sortField, asc)

	h.learn.Echo(ctx, processes.CLIEquivalent(),
		"Lists all running processes with CPU and memory usage from /proc")

	writeJSON(w, map[string]interface{}{
		"processes": procs,
		"count":     len(procs),
	})
}

// Kill handles POST /api/processes/{pid}/kill
func (h *ProcessesHandler) Kill(w http.ResponseWriter, r *http.Request) {
	// chi URL param requires the router to be set up with {pid}
	// We read it from the URL path manually for now
	pid := chi.URLParam(r, "pid")

	ctx := learn.WithContext(r.Context(), "processes")
	h.learn.Echo(ctx,
		"kill -TERM "+pid,
		"Sends SIGTERM to process "+pid+", asking it to terminate gracefully")

	// TODO: implement actual kill — requires careful permission checking
	writeJSON(w, map[string]interface{}{
		"ok":  false,
		"msg": "kill not yet implemented — coming next sprint",
	})
}
