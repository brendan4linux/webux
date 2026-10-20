package servicemap

import (
	"database/sql"
	"log/slog"
	"time"
)

// Watcher polls conntrack periodically and aggregates connection stats into SQLite.
type Watcher struct {
	db       *sql.DB
	interval time.Duration
}

// NewWatcher creates a watcher that writes to the given DB.
func NewWatcher(db *sql.DB) *Watcher {
	return &Watcher{db: db, interval: 30 * time.Second}
}

// Run starts the background polling loop. Call in a goroutine; it runs until
// the process exits (no context plumbing to keep it simple).
func (w *Watcher) Run() {
	// First tick immediately so data is available on first page load.
	w.tick()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for range ticker.C {
		w.tick()
	}
}

func (w *Watcher) tick() {
	entries, ok := readConntrack()
	if !ok {
		return // conntrack not available on this kernel
	}

	// Build inode→PID map for process attribution (best-effort; errors ignored)
	snapshot, err := Capture()
	if err != nil {
		slog.Debug("servicemap watcher: capture failed", "err", err)
		return
	}

	// Build a quick port→processName map from the snapshot
	portToProcess := map[int]string{}
	for _, svc := range snapshot.Services {
		portToProcess[svc.Port] = svc.Name
	}

	for _, e := range entries {
		// Skip loopback and non-outbound entries
		if e.DstIP == "127.0.0.1" || e.DstIP == "::1" {
			continue
		}
		// Skip internal-only (both src and dst are local) — we only want outbound
		if isPrivate(e.SrcIP) && isPrivate(e.DstIP) {
			// Still track internal service-to-service calls
		}

		srcProcess := portToProcess[e.SrcPort]
		if srcProcess == "" {
			srcProcess = "(unknown)"
		}
		hint := portServiceHint(e.DstPort)

		w.upsert(srcProcess, e.DstIP, e.DstPort, e.Protocol, hint)
	}
}

func (w *Watcher) upsert(srcProcess, dstIP string, dstPort int, protocol, hint string) {
	_, err := w.db.Exec(`
		INSERT INTO sm_connection_stats (src_process, dst_ip, dst_port, protocol, service_hint, hit_count, last_seen)
		VALUES (?, ?, ?, ?, ?, 1, datetime('now'))
		ON CONFLICT(src_process, dst_ip, dst_port, protocol)
		DO UPDATE SET
			hit_count    = hit_count + 1,
			service_hint = excluded.service_hint,
			last_seen    = datetime('now')
	`, srcProcess, dstIP, dstPort, protocol, hint)
	if err != nil {
		slog.Debug("servicemap upsert failed", "err", err)
	}
}

// isPrivate returns true for RFC1918 / link-local addresses.
func isPrivate(ip string) bool {
	switch {
	case len(ip) == 0:
		return false
	case ip == "127.0.0.1", ip == "::1":
		return true
	}
	// Simple prefix checks covering 10.x, 172.16-31.x, 192.168.x, 169.254.x
	for _, prefix := range []string{"10.", "172.1", "172.2", "172.3", "192.168.", "169.254.", "fc", "fd"} {
		if len(ip) >= len(prefix) && ip[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
