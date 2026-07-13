package handlers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/brendan4linux/webux/internal/system/logs"
)

// LogsHandler serves log file browsing and live-tail endpoints.
type LogsHandler struct{}

func NewLogsHandler() *LogsHandler { return &LogsHandler{} }

// List handles GET /api/logs/files?path=
// Returns immediate children of the requested directory under /var/log.
func (h *LogsHandler) List(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	entries, err := logs.ListDir(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, map[string]interface{}{"entries": entries})
}

// Read handles GET /api/logs/read?path=&lines=500
// Returns the last N lines of a log file.
func (h *LogsHandler) Read(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	lines := 500
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 {
		lines = n
	}
	out, err := logs.ReadTail(path, lines)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	writeJSON(w, map[string]interface{}{"lines": out, "count": len(out)})
}

// Follow handles GET /api/logs/follow?path=
// Streams new log lines as Server-Sent Events until the client disconnects.
func (h *LogsHandler) Follow(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	ctx := r.Context()
	pr, pw := io.Pipe()
	defer pr.Close()

	go func() {
		_ = logs.FollowFile(ctx, path, pw)
		pw.Close()
	}()

	streamSSE(ctx, pr, w, flusher)
}

// Units handles GET /api/logs/systemd/units
// Returns all systemd service units (empty list if systemd not present).
func (h *LogsHandler) Units(w http.ResponseWriter, r *http.Request) {
	units, err := logs.ListUnits()
	if err != nil || units == nil {
		writeJSON(w, map[string]interface{}{"units": []interface{}{}})
		return
	}
	writeJSON(w, map[string]interface{}{"units": units})
}

// FollowUnit handles GET /api/logs/systemd/follow?unit=
// Streams journalctl output for a unit as Server-Sent Events.
func (h *LogsHandler) FollowUnit(w http.ResponseWriter, r *http.Request) {
	unit := r.URL.Query().Get("unit")
	if unit == "" {
		http.Error(w, "unit required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	ctx := r.Context()
	pr, pw := io.Pipe()
	defer pr.Close()

	go func() {
		_ = logs.FollowUnit(ctx, unit, pw)
		pw.Close()
	}()

	streamSSE(ctx, pr, w, flusher)
}

// streamSSE reads from r and writes each non-empty line as an SSE data event.
func streamSSE(ctx context.Context, r io.Reader, w http.ResponseWriter, flusher http.Flusher) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			for _, line := range splitLines(string(buf[:n])) {
				fmt.Fprintf(w, "data: %s\n\n", line)
			}
			flusher.Flush()
		}
		if err != nil {
			return
		}
	}
}


// splitLines splits a chunk on newlines, trimming empty trailing entries.
func splitLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
