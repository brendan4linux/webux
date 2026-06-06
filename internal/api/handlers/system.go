package handlers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brendan4linux/webux/internal/system"
)

// SystemHandler serves host info and live metrics.
type SystemHandler struct {
	hostInfo *system.HostInfo
}

func NewSystemHandler(h *system.HostInfo) *SystemHandler {
	return &SystemHandler{hostInfo: h}
}

// Info handles GET /api/system/info
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"hostname":    hostname,
		"distro":      h.hostInfo.Distro,
		"arch":        h.hostInfo.Arch,
		"kernel":      h.hostInfo.Kernel,
		"init_system": h.hostInfo.InitSystem,
		"has_docker":  h.hostInfo.HasDocker,
		"has_podman":  h.hostInfo.HasPodman,
		"has_ansible": h.hostInfo.HasAnsible,
		"has_puppet":  h.hostInfo.HasPuppet,
		"has_ufw":     h.hostInfo.HasUFW,
		"has_nftables":h.hostInfo.HasNFTables,
		"has_iptables":h.hostInfo.HasIPTables,
	})
}

// Stats handles GET /api/system/stats — reads directly from /proc
func (h *SystemHandler) Stats(w http.ResponseWriter, r *http.Request) {
	stats, err := collectStats()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

type SystemStats struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemUsedMB     float64 `json:"mem_used_mb"`
	MemTotalMB    float64 `json:"mem_total_mb"`
	LoadAvg1      float64 `json:"load_avg_1"`
	LoadAvg5      float64 `json:"load_avg_5"`
	LoadAvg15     float64 `json:"load_avg_15"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	DiskUsedGB    float64 `json:"disk_used_gb"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
}

func collectStats() (*SystemStats, error) {
	s := &SystemStats{}

	// CPU: two /proc/stat samples 200ms apart for a meaningful delta
	idle1, total1, err := readCPUStat()
	if err != nil {
		return nil, err
	}
	time.Sleep(200 * time.Millisecond)
	idle2, total2, err := readCPUStat()
	if err != nil {
		return nil, err
	}
	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta > 0 {
		s.CPUPercent = (1 - idleDelta/totalDelta) * 100
	}

	// Memory from /proc/meminfo
	memInfo, err := readMemInfo()
	if err == nil {
		total := memInfo["MemTotal"]
		free := memInfo["MemFree"]
		buffers := memInfo["Buffers"]
		cached := memInfo["Cached"]
		sReclaimable := memInfo["SReclaimable"]
		used := total - free - buffers - cached - sReclaimable
		s.MemTotalMB = float64(total) / 1024
		s.MemUsedMB = float64(used) / 1024
	}

	// Load averages from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			s.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			s.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			s.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Uptime from /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			s.UptimeSeconds, _ = strconv.ParseFloat(fields[0], 64)
		}
	}

	// Disk usage for / via statfs syscall
	diskUsed, diskTotal, err := readDiskUsage("/")
	if err == nil {
		s.DiskUsedGB = float64(diskUsed) / 1e9
		s.DiskTotalGB = float64(diskTotal) / 1e9
	}

	return s, nil
}

func readCPUStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, fmt.Errorf("unexpected /proc/stat format")
		}
		// cpu user nice system idle iowait irq softirq steal guest guest_nice
		vals := make([]uint64, len(fields)-1)
		for i, f := range fields[1:] {
			vals[i], _ = strconv.ParseUint(f, 10, 64)
		}
		idle = vals[3] // idle
		if len(vals) > 4 {
			idle += vals[4] // iowait counts as idle for CPU usage purposes
		}
		for _, v := range vals {
			total += v
		}
		return idle, total, nil
	}
	return 0, 0, fmt.Errorf("cpu line not found in /proc/stat")
}

func readMemInfo() (map[string]uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string]uint64)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		valStr := strings.TrimSpace(strings.Replace(parts[1], "kB", "", 1))
		val, err := strconv.ParseUint(strings.TrimSpace(valStr), 10, 64)
		if err == nil {
			m[key] = val
		}
	}
	return m, scanner.Err()
}
