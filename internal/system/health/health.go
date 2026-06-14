// Package health runs configurable binary pass/fail system checks.
// Each check executes a shell command and uses the exit code (0=pass, non-zero=fail).
// A second "detail" command provides expanded information shown on click.
package health

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Check defines a single health check.
type Check struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Command       string `json:"command"`        // shell command: exit 0 = pass
	DetailCommand string `json:"detail_command"` // shell command for expanded detail
	Enabled       bool   `json:"enabled"`
}

// Result is the outcome of running a check.
type Result struct {
	Check
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"` // output of detail_command
	Error  string `json:"error,omitempty"`
	Ms     int64  `json:"ms"` // how long it took
}

// DefaultChecks returns the built-in health checks.
func DefaultChecks() []Check {
	return []Check{
		{
			ID:          "kernel_age",
			Label:       "Kernel / uptime",
			Description: "Uptime under 90 days — longer may mean pending kernel patches",
			Command:     `test $(awk '{print int($1/86400)}' /proc/uptime) -lt 90`,
			DetailCommand: `printf "Uptime:  "; uptime -p 2>/dev/null || awk '{d=int($1/86400);h=int(($1%86400)/3600);printf "%d days %d hours\n",d,h}' /proc/uptime; printf "Kernel:  "; uname -r; printf "Boot:    "; who -b 2>/dev/null | awk '{print $3,$4}' || uptime`,
			Enabled: true,
		},
		{
			ID:          "failed_services",
			Label:       "System services",
			Description: "No failed systemd units",
			Command:     `! systemctl --failed --no-legend 2>/dev/null | grep -q .`,
			DetailCommand: `failed=$(systemctl --failed --no-legend 2>/dev/null); if [ -z "$failed" ]; then echo "All services running normally"; else echo "$failed"; fi`,
			Enabled: true,
		},
		{
			ID:          "swap_pressure",
			Label:       "Memory / swap",
			Description: "Swap usage under 75%",
			Command:     `awk '/SwapTotal/{t=$2} /SwapFree/{f=$2} END{if(t==0)exit 0; exit (t-f)*100/t >= 75}' /proc/meminfo`,
			DetailCommand: `awk '/MemTotal/{mt=$2} /MemAvailable/{ma=$2} /SwapTotal/{st=$2} /SwapFree/{sf=$2} END{printf "RAM:   %.1f GB used / %.1f GB total\nSwap:  %.1f GB used / %.1f GB total\n",(mt-ma)/1048576,mt/1048576,(st-sf)/1048576,st/1048576}' /proc/meminfo`,
			Enabled: true,
		},
		{
			ID:          "disk_usage",
			Label:       "Disk usage",
			Description: "Real filesystems under 80% full",
			Command:     `df -lx tmpfs -x devtmpfs -x squashfs 2>/dev/null | awk 'NR>1 && $5+0 >= 80 {found=1} END{exit found+0}'`,
			DetailCommand: `df -lhx tmpfs -x devtmpfs -x squashfs 2>/dev/null | awk 'NR==1{print} NR>1{used=$5+0; flag=(used>=80)?" ⚠":""; print $0 flag}'`,
			Enabled: true,
		},
		{
			ID:          "load_avg",
			Label:       "CPU load",
			Description: "15-minute load average under number of CPU cores",
			Command:     fmt.Sprintf(`awk '{print ($3 < %d) ? 0 : 1}' /proc/loadavg | grep -q 0`, runtime.NumCPU()),
			DetailCommand: `cores=$(nproc); load=$(awk '{print $1,$2,$3}' /proc/loadavg); printf "Load avg (1/5/15 min): %s\nCPU cores: %d\n" "$load" "$cores"`,
			Enabled: true,
		},
		{
			ID:          "security_updates",
			Label:       "Security updates",
			Description: "No pending security updates",
			Command:     securityUpdateCmd(),
			DetailCommand: securityDetailCmd(),
			Enabled: true,
		},
	}
}

// securityUpdateCmd returns a distro-agnostic command to check for security updates.
func securityUpdateCmd() string {
	return `
if command -v checkupdates >/dev/null 2>&1; then
  # Arch — all updates are security-relevant
  ! checkupdates 2>/dev/null | grep -q .
elif command -v apt-get >/dev/null 2>&1; then
  ! apt-get -s upgrade 2>/dev/null | grep -qi "security"
elif command -v dnf >/dev/null 2>&1; then
  ! dnf check-update --security -q 2>/dev/null | grep -q .
elif command -v yum >/dev/null 2>&1; then
  ! yum check-update --security -q 2>/dev/null | grep -q .
else
  exit 0
fi`
}

func securityDetailCmd() string {
	return `
if command -v checkupdates >/dev/null 2>&1; then
  pkgs=$(checkupdates 2>/dev/null | wc -l); echo "Pending updates: $pkgs packages"
  checkupdates 2>/dev/null | head -10
elif command -v apt-get >/dev/null 2>&1; then
  apt-get -s upgrade 2>/dev/null | grep "^Inst" | grep -i security | head -10 || echo "No security updates pending"
elif command -v dnf >/dev/null 2>&1; then
  dnf check-update --security -q 2>/dev/null | head -10 || echo "No security updates pending"
else
  echo "Package manager not detected"
fi`
}

// ── Runner ────────────────────────────────────────────────────────────────

// Run executes a slice of checks concurrently and returns results.
func Run(checks []Check) []Result {
	results := make([]Result, len(checks))
	done := make(chan struct{}, len(checks))

	for i, c := range checks {
		go func(idx int, ch Check) {
			results[idx] = runOne(ch)
			done <- struct{}{}
		}(i, c)
	}
	for range checks {
		<-done
	}
	return results
}

// RunDetail runs just the detail command for a single check ID.
func RunDetail(checks []Check, id string) (string, error) {
	for _, c := range checks {
		if c.ID == id {
			return runDetailCmd(c.DetailCommand)
		}
	}
	return "", fmt.Errorf("check %q not found", id)
}

func runOne(c Check) Result {
	r := Result{Check: c}
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", c.Command)
	err := cmd.Run()
	r.Ms = time.Since(start).Milliseconds()

	if ctx.Err() != nil {
		r.Pass = false
		r.Error = "timed out"
		return r
	}
	r.Pass = (err == nil)
	return r
}

func runDetailCmd(cmd string) (string, error) {
	if cmd == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ── Settings persistence ──────────────────────────────────────────────────

// LoadChecks loads checks from JSON string, falling back to defaults.
func LoadChecks(jsonStr string) []Check {
	if jsonStr == "" {
		return DefaultChecks()
	}
	var checks []Check
	if err := json.Unmarshal([]byte(jsonStr), &checks); err != nil {
		return DefaultChecks()
	}
	return checks
}

// MarshalChecks serialises checks to JSON for storage.
func MarshalChecks(checks []Check) string {
	b, _ := json.MarshalIndent(checks, "", "  ")
	return string(b)
}

// ── Helpers ───────────────────────────────────────────────────────────────

// UptimeDays returns the system uptime in days.
func UptimeDays() int {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	secs, _ := strconv.ParseFloat(fields[0], 64)
	return int(secs / 86400)
}

// NumCPU returns the number of logical CPUs.
func NumCPU() int { return runtime.NumCPU() }

// used by detail commands
var _ = bufio.NewScanner
var _ = bytes.NewReader
