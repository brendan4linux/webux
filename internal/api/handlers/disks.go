package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

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

// DirUsage handles GET /api/disks/usage?mount=/&top=20
// Returns the largest immediate subdirectories of a mount point,
// sized with a full recursive walk device-locked to that partition.
func (h *DisksHandler) DirUsage(w http.ResponseWriter, r *http.Request) {
	mount := filepath.Clean(r.URL.Query().Get("mount"))
	if mount == "" || mount == "." {
		http.Error(w, "mount param required", http.StatusBadRequest)
		return
	}
	top := queryIntDisk(r, "top", disks.DefaultUsageTopN)

	// Resolve partition total from df so % values are meaningful.
	var partTotal int64
	if summary, err := disks.Scan(); err == nil {
		for _, m := range summary.Mounts {
			if m.MountPoint == mount {
				partTotal = m.Total
				break
			}
		}
	}

	entries, err := disks.SubDirs(mount, mount, top, partTotal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := learn.WithContext(r.Context(), "disks")
	h.learn.Echo(ctx,
		"du -x --max-depth=1 "+mount+" | sort -rh | head -"+strconv.Itoa(top),
		"Shows largest directories on the selected partition without crossing filesystem boundaries")

	writeJSON(w, map[string]any{
		"mount":   mount,
		"entries": entries,
	})
}

// DirDrillDown handles GET /api/disks/drilldown?mount=/&path=/home&top=20
// Returns the largest immediate subdirectories of path, device-locked to mount.
func (h *DisksHandler) DirDrillDown(w http.ResponseWriter, r *http.Request) {
	mount := filepath.Clean(r.URL.Query().Get("mount"))
	target := filepath.Clean(r.URL.Query().Get("path"))
	top := queryIntDisk(r, "top", disks.DefaultUsageTopN)

	if mount == "" || mount == "." {
		http.Error(w, "mount param required", http.StatusBadRequest)
		return
	}
	if target == "" || target == "." {
		target = mount
	}
	if !strings.HasPrefix(target, mount) {
		http.Error(w, "path must be within mount", http.StatusBadRequest)
		return
	}

	var partTotal int64
	if summary, err := disks.Scan(); err == nil {
		for _, m := range summary.Mounts {
			if m.MountPoint == mount {
				partTotal = m.Total
				break
			}
		}
	}

	entries, err := disks.SubDirs(mount, target, top, partTotal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := learn.WithContext(r.Context(), "disks")
	h.learn.Echo(ctx,
		"du -x --max-depth=1 "+target+" | sort -rh | head -"+strconv.Itoa(top),
		"Drills into "+target+" showing largest subdirectories, staying on the same filesystem")

	writeJSON(w, map[string]any{
		"mount":   mount,
		"path":    target,
		"entries": entries,
	})
}

func queryIntDisk(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
