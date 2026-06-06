package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/containers"
)

// ContainersHandler manages Docker and Podman containers.
type ContainersHandler struct {
	runtimes []containers.Runtime
	learn    *learn.Store
}

func NewContainersHandler(ls *learn.Store) *ContainersHandler {
	return &ContainersHandler{
		runtimes: containers.Detect(),
		learn:    ls,
	}
}

// runtimeFor returns the first runtime matching the given name, or the first available.
func (h *ContainersHandler) runtimeFor(name string) (containers.Runtime, error) {
	if len(h.runtimes) == 0 {
		return nil, fmt.Errorf("no container runtime detected (Docker or Podman socket not found)")
	}
	if name == "" {
		return h.runtimes[0], nil
	}
	for _, r := range h.runtimes {
		if r.Name() == name {
			return r, nil
		}
	}
	return h.runtimes[0], nil
}

// Status handles GET /api/containers/status
func (h *ContainersHandler) Status(w http.ResponseWriter, r *http.Request) {
	var runtimeNames []string
	for _, rt := range h.runtimes {
		runtimeNames = append(runtimeNames, rt.Name())
	}
	writeJSON(w, map[string]interface{}{
		"runtimes": runtimeNames,
		"count":    len(h.runtimes),
	})
}

// List handles GET /api/containers?runtime=docker&all=true
func (h *ContainersHandler) List(w http.ResponseWriter, r *http.Request) {
	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		writeJSON(w, map[string]interface{}{"containers": []interface{}{}, "error": err.Error()})
		return
	}
	all := r.URL.Query().Get("all") == "true"
	ctx := learn.WithContext(r.Context(), "containers")

	cs, err := rt.ListContainers(ctx, all)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cmd := fmt.Sprintf("%s ps", rt.Name())
	if all {
		cmd += " -a"
	}
	h.learn.Echo(ctx, cmd, "Lists all containers via the "+rt.Name()+" socket API")
	writeJSON(w, map[string]interface{}{"containers": cs, "runtime": rt.Name(), "count": len(cs)})
}

// Action handles POST /api/containers/{id}/action
// Body: {"action":"start"|"stop"|"restart"|"remove","force":false}
func (h *ContainersHandler) Action(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Action  string `json:"action"`
		Force   bool   `json:"force"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	ctx := learn.WithContext(r.Context(), "containers")
	var cliCmd string

	switch body.Action {
	case "start":
		cliCmd, err = rt.Start(ctx, id)
	case "stop":
		cliCmd, err = rt.Stop(ctx, id, body.Timeout)
	case "restart":
		cliCmd, err = rt.Restart(ctx, id)
	case "remove":
		cliCmd, err = rt.Remove(ctx, id, body.Force)
	default:
		http.Error(w, "unknown action: "+body.Action, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	h.learn.Echo(ctx, cliCmd, strings.Title(body.Action)+"s container "+id[:min(12, len(id))])
	writeJSON(w, map[string]interface{}{"ok": true, "cli_cmd": cliCmd})
}

// Logs handles GET /api/containers/{id}/logs?tail=100
// Streams as SSE for live tailing.
func (h *ContainersHandler) Logs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	tail := 100
	if t := r.URL.Query().Get("tail"); t != "" {
		fmt.Sscanf(t, "%d", &tail)
	}

	ctx := learn.WithContext(r.Context(), "containers")
	h.learn.Echo(ctx,
		fmt.Sprintf("%s logs --tail %d %s", rt.Name(), tail, id[:min(12, len(id))]),
		"Fetches the last "+itoa(tail)+" log lines for container "+id[:min(12, len(id))])

	logs, err := rt.Logs(ctx, id, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer logs.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		line := scanner.Text()
		// Docker log stream has 8-byte header — strip it for display
		if len(line) > 8 {
			b := []byte(line)
			if b[0] <= 2 { // stdout=1, stderr=2
				line = string(b[8:])
			}
		}
		fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", " "))
		if ok {
			flusher.Flush()
		}
	}
}

// ListImages handles GET /api/containers/images
func (h *ContainersHandler) ListImages(w http.ResponseWriter, r *http.Request) {
	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		writeJSON(w, map[string]interface{}{"images": []interface{}{}, "error": err.Error()})
		return
	}
	ctx := learn.WithContext(r.Context(), "containers")
	images, err := rt.ListImages(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx, rt.Name()+" images", "Lists all local container images")
	writeJSON(w, map[string]interface{}{"images": images, "runtime": rt.Name()})
}

// Stats handles GET /api/containers/{id}/stats
func (h *ContainersHandler) Stats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rt, err := h.runtimeFor(r.URL.Query().Get("runtime"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	stats, err := rt.ContainerStats(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, stats)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
