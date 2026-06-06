package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/brendan4linux/webux/internal/learn"
	"github.com/brendan4linux/webux/internal/system/disks"
)

type DisksHandler struct {
	learn *learn.Store
}

func NewDisksHandler(ls *learn.Store) *DisksHandler {
	return &DisksHandler{learn: ls}
}

func (h *DisksHandler) Summary(w http.ResponseWriter, r *http.Request) {
	ctx := learn.WithContext(r.Context(), "disks")
	summary, err := disks.Scan()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.learn.Echo(ctx,
		"lsblk -J && df -Ph && vgs --reportformat json && lvs --reportformat json",
		"Scans all block devices, mounts, and LVM volume groups")
	writeJSON(w, summary)
}

func (h *DisksHandler) Extend(w http.ResponseWriter, r *http.Request) {
	var opts disks.ExtendOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)

	ctx := learn.WithContext(r.Context(), "disks")
	out := make(chan string, 32)

	go func() {
		err := disks.Extend(r.Context(), opts, out)
		if err != nil {
			out <- "[error] " + err.Error()
		}
		h.learn.Echo(ctx,
			fmt.Sprintf("lvextend -L +%.2fG %s", opts.SizeGB, opts.LVPath),
			"Extends LVM logical volume and resizes filesystem")
		close(out)
	}()

	for line := range out {
		fmt.Fprintf(w, "data: %s\n\n", line)
		if ok { flusher.Flush() }
	}
}
