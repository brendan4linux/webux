// Package performance provides kernel/system performance tuning checks.
package performance

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Check struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"`
	Points   int    `json:"points"`
}

type Result struct {
	Check
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

type Score struct {
	Results  []Result  `json:"results"`
	Raw      int       `json:"raw"`
	Max      int       `json:"max"`
	Pct      int       `json:"pct"`
	Level    string    `json:"level"`
	Color    string    `json:"color"`
	Rank     string    `json:"rank"`
	RankIcon string    `json:"rank_icon"`
	RunAt    time.Time `json:"run_at"`
}

func (s *Score) MarshalJSON() ([]byte, error) {
	type Alias Score
	return json.Marshal((*Alias)(s))
}

func DefaultChecks() []Check {
	return []Check{
		{ID: "vm_swappiness", Label: "vm.swappiness ≤ 30", Category: "memory", Points: 10},
		{ID: "vm_vfs_cache_pressure", Label: "vm.vfs_cache_pressure ≤ 75", Category: "memory", Points: 8},
		{ID: "vm_dirty_background_ratio", Label: "vm.dirty_background_ratio ≤ 10", Category: "memory", Points: 5},
		{ID: "fs_file_max", Label: "fs.file-max ≥ 2097152", Category: "filesystem", Points: 8},
		{ID: "net_netdev_max_backlog", Label: "net.core.netdev_max_backlog ≥ 5000", Category: "network", Points: 8},
		{ID: "net_somaxconn", Label: "net.core.somaxconn ≥ 4096", Category: "network", Points: 8},
		{ID: "net_tcp_max_syn_backlog", Label: "net.ipv4.tcp_max_syn_backlog ≥ 4096", Category: "network", Points: 7},
		{ID: "net_rmem_max", Label: "net.core.rmem_max ≥ 4 MiB", Category: "network", Points: 6},
		{ID: "net_wmem_max", Label: "net.core.wmem_max ≥ 4 MiB", Category: "network", Points: 6},
		{ID: "net_tcp_fin_timeout", Label: "net.ipv4.tcp_fin_timeout ≤ 30", Category: "network", Points: 6},
		{ID: "net_ip_local_port_range", Label: "ip_local_port_range upper ≥ 65000", Category: "network", Points: 5},
		{ID: "kernel_pid_max", Label: "kernel.pid_max ≥ 131072", Category: "kernel", Points: 5},
		{ID: "net_tcp_bbr", Label: "TCP congestion control: BBR", Category: "network", Points: 8},
		{ID: "systemd_nofile_hard", Label: "systemd DefaultLimitNOFILE hard ≥ 1048576", Category: "systemd", Points: 8},
		{ID: "net_tcp_slow_start", Label: "tcp_slow_start_after_idle = 0", Category: "network", Points: 6},
	}
}

func RunAll() *Score {
	checks := DefaultChecks()
	results := make([]Result, len(checks))

	var wg sync.WaitGroup
	for i, ch := range checks {
		wg.Add(1)
		go func(idx int, c Check) {
			defer wg.Done()
			pass, detail := runCheck(c.ID)
			results[idx] = Result{Check: c, Pass: pass, Detail: detail}
		}(i, ch)
	}
	wg.Wait()

	raw, max := 0, 0
	for _, r := range results {
		max += r.Points
		if r.Pass {
			raw += r.Points
		}
	}
	pct := 0
	if max > 0 {
		pct = raw * 100 / max
	}

	rank, rankIcon := scoreRank(pct)
	return &Score{
		Results:  results,
		Raw:      raw,
		Max:      max,
		Pct:      pct,
		Level:    scoreLevel(pct),
		Color:    levelColor(pct),
		Rank:     rank,
		RankIcon: rankIcon,
		RunAt:    time.Now(),
	}
}

func scoreLevel(pct int) string {
	switch {
	case pct >= 76:
		return "Tuned"
	case pct >= 51:
		return "Decent"
	case pct >= 26:
		return "Default"
	default:
		return "Sluggish"
	}
}

func scoreRank(pct int) (string, string) {
	switch {
	case pct >= 81:
		return "Platinum", "🏆"
	case pct >= 71:
		return "Gold", "🥇"
	case pct >= 61:
		return "Silver", "🥈"
	default:
		return "Bronze", "🥉"
	}
}

func levelColor(pct int) string {
	switch {
	case pct >= 76:
		return "#4ade80"
	case pct >= 51:
		return "#facc15"
	case pct >= 26:
		return "#fb923c"
	default:
		return "#f87171"
	}
}

func sysctl(key string) (string, error) {
	path := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func sysctlInt(key string) (int64, error) {
	v, err := sysctl(key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func runCheck(id string) (bool, string) {
	switch id {
	case "vm_swappiness":
		v, err := sysctlInt("vm.swappiness")
		if err != nil {
			return false, "Cannot read vm.swappiness"
		}
		if v <= 30 {
			return true, fmt.Sprintf("vm.swappiness = %d (≤ 30 — prefers RAM over swap)", v)
		}
		return false, fmt.Sprintf("vm.swappiness = %d — set to ≤ 30 to prefer RAM: sysctl -w vm.swappiness=10", v)

	case "vm_vfs_cache_pressure":
		v, err := sysctlInt("vm.vfs_cache_pressure")
		if err != nil {
			return false, "Cannot read vm.vfs_cache_pressure"
		}
		if v <= 75 {
			return true, fmt.Sprintf("vm.vfs_cache_pressure = %d (≤ 75 — retains inode/dentry cache longer)", v)
		}
		return false, fmt.Sprintf("vm.vfs_cache_pressure = %d — set to ≤ 75: sysctl -w vm.vfs_cache_pressure=50", v)

	case "vm_dirty_background_ratio":
		v, err := sysctlInt("vm.dirty_background_ratio")
		if err != nil {
			return false, "Cannot read vm.dirty_background_ratio"
		}
		if v <= 10 {
			return true, fmt.Sprintf("vm.dirty_background_ratio = %d%% (starts background writeback early)", v)
		}
		return false, fmt.Sprintf("vm.dirty_background_ratio = %d%% — set to ≤ 10: sysctl -w vm.dirty_background_ratio=5", v)

	case "fs_file_max":
		v, err := sysctlInt("fs.file-max")
		if err != nil {
			return false, "Cannot read fs.file-max"
		}
		if v >= 2097152 {
			return true, fmt.Sprintf("fs.file-max = %d (≥ 2097152 — high open-file limit)", v)
		}
		return false, fmt.Sprintf("fs.file-max = %d — set to ≥ 2097152: sysctl -w fs.file-max=2097152", v)

	case "net_netdev_max_backlog":
		v, err := sysctlInt("net.core.netdev_max_backlog")
		if err != nil {
			return false, "Cannot read net.core.netdev_max_backlog"
		}
		if v >= 5000 {
			return true, fmt.Sprintf("net.core.netdev_max_backlog = %d (≥ 5000 — handles NIC bursts)", v)
		}
		return false, fmt.Sprintf("net.core.netdev_max_backlog = %d — set to ≥ 5000: sysctl -w net.core.netdev_max_backlog=5000", v)

	case "net_somaxconn":
		v, err := sysctlInt("net.core.somaxconn")
		if err != nil {
			return false, "Cannot read net.core.somaxconn"
		}
		if v >= 4096 {
			return true, fmt.Sprintf("net.core.somaxconn = %d (≥ 4096 — large listen backlog)", v)
		}
		return false, fmt.Sprintf("net.core.somaxconn = %d — set to ≥ 4096: sysctl -w net.core.somaxconn=4096", v)

	case "net_tcp_max_syn_backlog":
		v, err := sysctlInt("net.ipv4.tcp_max_syn_backlog")
		if err != nil {
			return false, "Cannot read net.ipv4.tcp_max_syn_backlog"
		}
		if v >= 4096 {
			return true, fmt.Sprintf("net.ipv4.tcp_max_syn_backlog = %d (≥ 4096 — large SYN queue)", v)
		}
		return false, fmt.Sprintf("net.ipv4.tcp_max_syn_backlog = %d — set to ≥ 4096: sysctl -w net.ipv4.tcp_max_syn_backlog=4096", v)

	case "net_rmem_max":
		v, err := sysctlInt("net.core.rmem_max")
		if err != nil {
			return false, "Cannot read net.core.rmem_max"
		}
		if v >= 4194304 {
			return true, fmt.Sprintf("net.core.rmem_max = %d bytes (≥ 4 MiB recv buffer)", v)
		}
		return false, fmt.Sprintf("net.core.rmem_max = %d bytes — set to ≥ 4194304: sysctl -w net.core.rmem_max=4194304", v)

	case "net_wmem_max":
		v, err := sysctlInt("net.core.wmem_max")
		if err != nil {
			return false, "Cannot read net.core.wmem_max"
		}
		if v >= 4194304 {
			return true, fmt.Sprintf("net.core.wmem_max = %d bytes (≥ 4 MiB send buffer)", v)
		}
		return false, fmt.Sprintf("net.core.wmem_max = %d bytes — set to ≥ 4194304: sysctl -w net.core.wmem_max=4194304", v)

	case "net_tcp_fin_timeout":
		v, err := sysctlInt("net.ipv4.tcp_fin_timeout")
		if err != nil {
			return false, "Cannot read net.ipv4.tcp_fin_timeout"
		}
		if v <= 30 {
			return true, fmt.Sprintf("net.ipv4.tcp_fin_timeout = %ds (≤ 30s — faster TIME_WAIT reclaim)", v)
		}
		return false, fmt.Sprintf("net.ipv4.tcp_fin_timeout = %ds — set to ≤ 30: sysctl -w net.ipv4.tcp_fin_timeout=15", v)

	case "net_ip_local_port_range":
		v, err := sysctl("net.ipv4.ip_local_port_range")
		if err != nil {
			return false, "Cannot read net.ipv4.ip_local_port_range"
		}
		parts := strings.Fields(v)
		if len(parts) == 2 {
			upper, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil && upper >= 65000 {
				return true, fmt.Sprintf("ip_local_port_range = %s (upper ≥ 65000 — large ephemeral port range)", v)
			}
		}
		return false, fmt.Sprintf("ip_local_port_range = %s — set upper to ≥ 65000: sysctl -w net.ipv4.ip_local_port_range=\"1024 65535\"", v)

	case "kernel_pid_max":
		v, err := sysctlInt("kernel.pid_max")
		if err != nil {
			return false, "Cannot read kernel.pid_max"
		}
		if v >= 131072 {
			return true, fmt.Sprintf("kernel.pid_max = %d (≥ 131072 — supports large workloads)", v)
		}
		return false, fmt.Sprintf("kernel.pid_max = %d — set to ≥ 131072: sysctl -w kernel.pid_max=131072", v)

	case "net_tcp_bbr":
		v, err := sysctl("net.ipv4.tcp_congestion_control")
		if err != nil {
			return false, "Cannot read tcp_congestion_control"
		}
		if strings.TrimSpace(v) == "bbr" {
			return true, "TCP congestion control is BBR (better throughput on modern kernels)"
		}
		return false, fmt.Sprintf("tcp_congestion_control = %s — enable BBR: modprobe tcp_bbr && sysctl -w net.ipv4.tcp_congestion_control=bbr", v)

	case "systemd_nofile_hard":
		for _, p := range []string{"/etc/systemd/system.conf", "/etc/systemd/system.conf.d/limits.conf"} {
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "#") {
					continue
				}
				if !strings.HasPrefix(strings.ToLower(line), "defaultlimitnofile") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) < 2 {
					continue
				}
				val := strings.TrimSpace(parts[1])
				// format: soft:hard or just value
				limits := strings.SplitN(val, ":", 2)
				hardStr := limits[len(limits)-1]
				hard, err := strconv.ParseInt(strings.TrimSpace(hardStr), 10, 64)
				if err != nil {
					continue
				}
				if hard >= 1048576 {
					return true, fmt.Sprintf("DefaultLimitNOFILE hard = %d (≥ 1048576)", hard)
				}
				return false, fmt.Sprintf("DefaultLimitNOFILE hard = %d — set to ≥ 1048576 in /etc/systemd/system.conf", hard)
			}
		}
		return false, "DefaultLimitNOFILE not set in /etc/systemd/system.conf — add: DefaultLimitNOFILE=1048576:1048576"

	case "net_tcp_slow_start":
		v, err := sysctlInt("net.ipv4.tcp_slow_start_after_idle")
		if err != nil {
			return false, "Cannot read tcp_slow_start_after_idle (kernel may not support it)"
		}
		if v == 0 {
			return true, "tcp_slow_start_after_idle = 0 (disabled — better for long-lived connections)"
		}
		return false, fmt.Sprintf("tcp_slow_start_after_idle = %d — disable: sysctl -w net.ipv4.tcp_slow_start_after_idle=0", v)
	}

	return false, fmt.Sprintf("unknown check id: %s", id)
}
