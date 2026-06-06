package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/puppet"
)

type PuppetHandler struct {
	agent *puppet.Agent
	learn *learn.Store
}

func NewPuppetHandler(ls *learn.Store) *PuppetHandler {
	return &PuppetHandler{agent: puppet.NewAgent(), learn: ls}
}

// Status handles GET /api/puppet/status
func (h *PuppetHandler) Status(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "puppet")
	status, err := h.agent.Status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	clicmds := puppet.CLIEquivalents()
	h.learn.Echo(ctx, clicmds["status"],
		"Reads Puppet agent status from config, lockfile, and last_run_summary.yaml")
	writeJSON(w, status)
}

// Catalog handles GET /api/puppet/catalog
func (h *PuppetHandler) Catalog(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "puppet")
	resources, path, err := h.agent.CatalogResources()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.learn.Echo(ctx, puppet.CLIEquivalents()["catalog"],
		"Parses the compiled Puppet catalog for this node")
	writeJSON(w, map[string]interface{}{
		"resources": resources,
		"count":     len(resources),
		"path":      path,
	})
}

// Report handles GET /api/puppet/report
func (h *PuppetHandler) Report(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "puppet")
	events, err := h.agent.LastRunEvents()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.learn.Echo(ctx, puppet.CLIEquivalents()["report"],
		"Parses the last Puppet run report for per-resource events")
	writeJSON(w, map[string]interface{}{
		"events": events,
		"count":  len(events),
	})
}

// Facts handles GET /api/puppet/facts
func (h *PuppetHandler) Facts(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "puppet")
	facts, err := h.agent.Facts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, puppet.CLIEquivalents()["facts"],
		"Runs facter to collect all system facts")
	writeJSON(w, facts)
}

// Run handles POST /api/puppet/run — streams SSE output
func (h *PuppetHandler) Run(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Noop bool `json:"noop"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "puppet")
	out := make(chan string, 64)
	clicmds := puppet.CLIEquivalents()

	go func() {
		key := "run"
		if body.Noop {
			key = "noop"
		}
		cliCmd, err := h.agent.RunAgent(r.Context(), body.Noop, out)
		if err != nil {
			out <- fmt.Sprintf("[webux] error: %s", err.Error())
		}
		h.learn.Echo(ctx, cliCmd, "Triggers a Puppet agent run on this node")
		_ = clicmds[key]
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok {
			flusher.Flush()
		}
	}
}

// Enable handles POST /api/puppet/enable
func (h *PuppetHandler) Enable(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "puppet")
	cliCmd, err := h.agent.Enable()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Enables the Puppet agent to apply catalogs")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Disable handles POST /api/puppet/disable
func (h *PuppetHandler) Disable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Message string `json:"message"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	ctx := learn.WithContext(r.Context(), "puppet")
	cliCmd, err := h.agent.Disable(body.Message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, cliCmd, "Disables the Puppet agent — it will no longer apply catalogs")
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}
