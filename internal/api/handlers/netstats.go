package handlers

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// StreamInterfaceStats handles GET /api/network/interfaces/{name}/stats/stream
// It streams bandwidth samples as Server-Sent Events, one per second.
// Each event is a JSON object with rx_bytes_sec and tx_bytes_sec (bytes/sec rate).
func StreamInterfaceStats(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	// Verify SSE is supported by checking client accepts it
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering if proxied

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Read initial sample
	prev, err := readIfaceStats(name)
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: {\"error\": %q}\n\n", err.Error())
		flusher.Flush()
		return
	}
	prevTime := time.Now()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			curr, err := readIfaceStats(name)
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: {\"error\": %q}\n\n", err.Error())
				flusher.Flush()
				return
			}

			elapsed := t.Sub(prevTime).Seconds()
			if elapsed <= 0 {
				elapsed = 1
			}

			rxRate := float64(curr[0]-prev[0]) / elapsed
			txRate := float64(curr[8]-prev[8]) / elapsed
			if rxRate < 0 { rxRate = 0 } // counter wrap
			if txRate < 0 { txRate = 0 }

			fmt.Fprintf(w,
				"data: {\"rx_bytes_sec\":%.0f,\"tx_bytes_sec\":%.0f,\"rx_total\":%d,\"tx_total\":%d,\"ts\":%d}\n\n",
				rxRate, txRate, curr[0], curr[8], t.UnixMilli(),
			)
			flusher.Flush()

			prev = curr
			prevTime = t
		}
	}
}

// readIfaceStats reads the 16 counter fields for an interface from /proc/net/dev.
// Index 0 = rx_bytes, index 8 = tx_bytes (same layout as the full parser).
func readIfaceStats(name string) ([16]uint64, error) {
	var stats [16]uint64
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return stats, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // header 1
	scanner.Scan() // header 2

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		ifName := strings.TrimSpace(line[:colonIdx])
		if ifName != name {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		for i := 0; i < 16 && i < len(fields); i++ {
			fmt.Sscanf(fields[i], "%d", &stats[i])
		}
		return stats, nil
	}
	return stats, fmt.Errorf("interface %q not found in /proc/net/dev", name)
}
