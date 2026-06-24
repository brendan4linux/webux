// Package hardening provides security hardening checks and a scoring/leveling system.
package hardening

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Check defines a single hardening check with its point value.
type Check struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Category string `json:"category"` // "ssh" | "network" | "filesystem" | "updates" | "auth"
	Points   int    `json:"points"`
}

// Result is the outcome of running a Check.
type Result struct {
	Check
	Pass   bool   `json:"pass"`
	Detail string `json:"detail"`
}

// Score is the aggregated result of all checks.
type Score struct {
	Results []Result  `json:"results"`
	Raw      int       `json:"raw"`
	Max      int       `json:"max"`
	Pct      int       `json:"pct"`
	Level    string    `json:"level"`
	Color    string    `json:"color"`
	Rank     string    `json:"rank"`      // "Bronze" | "Silver" | "Gold" | "Platinum"
	RankIcon string    `json:"rank_icon"` // emoji medal
	RunAt    time.Time `json:"run_at"`
}

func (s *Score) MarshalJSON() ([]byte, error) {
	type Alias Score
	return json.Marshal((*Alias)(s))
}

// DefaultChecks returns the full set of hardening checks.
func DefaultChecks() []Check {
	return []Check{
		{ID: "ssh_no_password", Label: "SSH: password authentication disabled", Category: "ssh", Points: 20},
		{ID: "ssh_no_root", Label: "SSH: root login prohibited", Category: "ssh", Points: 15},
		{ID: "ssh_protocol2", Label: "SSH: protocol 2 only", Category: "ssh", Points: 5},
		{ID: "firewall_active", Label: "Firewall is active", Category: "network", Points: 20},
		{ID: "tmp_noexec", Label: "/tmp mounted with noexec", Category: "filesystem", Points: 10},
		{ID: "vartmp_noexec", Label: "/var/tmp mounted with noexec", Category: "filesystem", Points: 10},
		{ID: "auto_updates", Label: "Automatic security updates configured", Category: "updates", Points: 15},
		{ID: "intrusion_prevention", Label: "Intrusion prevention active (fail2ban/sshguard/crowdsec)", Category: "network", Points: 10},
		{ID: "no_empty_passwords", Label: "No accounts with empty passwords", Category: "auth", Points: 15},
	}
}

// RunAll executes all checks concurrently and returns a Score.
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
		return "Fortress"
	case pct >= 51:
		return "Hardened"
	case pct >= 26:
		return "Minimal"
	default:
		return "Exposed"
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

func runCheck(id string) (bool, string) {
	switch id {
	case "ssh_no_password":
		return checkSSHDirective("passwordauthentication", "no",
			"SSH password authentication is disabled",
			"Set `PasswordAuthentication no` in /etc/ssh/sshd_config and restart sshd")
	case "ssh_no_root":
		return checkSSHNoRoot()
	case "ssh_protocol2":
		cfg := readSSHDConfig()
		if strings.Contains(strings.ToLower(cfg), "protocol 1") {
			return false, "sshd_config explicitly enables Protocol 1 — critical vulnerability"
		}
		return true, "SSH Protocol 2 is in use (Protocol 1 not enabled)"
	case "firewall_active":
		return checkFirewall()
	case "tmp_noexec":
		return checkMountOption("/tmp", "noexec",
			"/tmp is mounted with noexec",
			"Add `noexec` to /tmp's fstab mount options and remount")
	case "vartmp_noexec":
		return checkMountOption("/var/tmp", "noexec",
			"/var/tmp is mounted with noexec",
			"Add `noexec` to /var/tmp's fstab mount options and remount")
	case "auto_updates":
		return checkAutoUpdates()
	case "intrusion_prevention":
		return checkIntrusionPrevention()
	case "no_empty_passwords":
		return checkNoEmptyPasswords()
	}
	return false, fmt.Sprintf("unknown check id: %s", id)
}

func readSSHDConfig() string {
	var sb strings.Builder
	for _, p := range []string{"/etc/ssh/sshd_config", "/etc/sshd_config"} {
		if b, err := os.ReadFile(p); err == nil {
			sb.Write(b)
		}
	}
	if matches, err := filepath.Glob("/etc/ssh/sshd_config.d/*.conf"); err == nil {
		for _, m := range matches {
			if b, err := os.ReadFile(m); err == nil {
				sb.Write(b)
			}
		}
	}
	return sb.String()
}

func checkSSHDirective(directive, wantValue, passMsg, failMsg string) (bool, string) {
	cfg := readSSHDConfig()
	if cfg == "" {
		return false, "Could not read sshd_config"
	}
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, directive) {
			fields := strings.Fields(lower)
			if len(fields) >= 2 {
				if fields[1] == wantValue {
					return true, passMsg
				}
				return false, failMsg + " (currently: " + fields[1] + ")"
			}
		}
	}
	return false, failMsg + " (directive not found in sshd_config)"
}

func checkSSHNoRoot() (bool, string) {
	cfg := readSSHDConfig()
	if cfg == "" {
		return false, "Could not read sshd_config"
	}
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "permitrootlogin") {
			fields := strings.Fields(lower)
			if len(fields) >= 2 {
				v := fields[1]
				switch v {
				case "no", "prohibit-password", "forced-commands-only":
					return true, "PermitRootLogin is set to: " + v
				default:
					return false, "PermitRootLogin is `" + v + "` — direct root SSH login is allowed. Set to `no` or `prohibit-password`"
				}
			}
		}
	}
	return true, "PermitRootLogin not set — modern OpenSSH defaults to prohibit-password"
}

func checkFirewall() (bool, string) {
	if out, err := exec.Command("ufw", "status").Output(); err == nil {
		if strings.Contains(strings.ToLower(string(out)), "status: active") {
			return true, "UFW firewall is active"
		}
		return false, "UFW is installed but not active — run `ufw enable`"
	}
	for _, svc := range []string{"firewalld", "nftables"} {
		if out, err := exec.Command("systemctl", "is-active", svc).Output(); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return true, svc + " is active"
			}
		}
	}
	if out, err := exec.Command("iptables", "-L", "-n").Output(); err == nil {
		count := 0
		for _, l := range strings.Split(string(out), "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "Chain") && !strings.HasPrefix(l, "target") {
				count++
			}
		}
		if count > 0 {
			return true, fmt.Sprintf("iptables has %d active rules", count)
		}
		return false, "iptables has no rules — firewall is inactive"
	}
	return false, "No active firewall detected (ufw, firewalld, nftables, or iptables)"
}

func checkMountOption(mountpoint, option, passMsg, failMsg string) (bool, string) {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false, "Could not read /proc/mounts"
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != mountpoint {
			continue
		}
		for _, o := range strings.Split(fields[3], ",") {
			if o == option {
				return true, passMsg
			}
		}
		return false, failMsg + " (current options: " + fields[3] + ")"
	}
	return false, mountpoint + " is not on a separate partition — parent mount may not have noexec"
}

func checkAutoUpdates() (bool, string) {
	checks := []struct{ bin, svc, label string }{
		{"unattended-upgrade", "unattended-upgrades", "unattended-upgrades (Debian/Ubuntu)"},
		{"", "dnf-automatic.timer", "dnf-automatic (RHEL/Fedora)"},
		{"", "arch-updates.timer", "arch-updates (Arch)"},
		{"", "yast2-online-update-configuration.service", "automatic updates (openSUSE)"},
	}
	for _, c := range checks {
		if c.bin != "" {
			if _, err := exec.LookPath(c.bin); err != nil {
				continue
			}
		}
		if out, err := exec.Command("systemctl", "is-enabled", c.svc).Output(); err == nil {
			if strings.TrimSpace(string(out)) == "enabled" {
				return true, c.label + " is enabled"
			}
		}
	}
	return false, "No automatic security update service detected — consider enabling unattended-upgrades or dnf-automatic"
}

func checkIntrusionPrevention() (bool, string) {
	for _, svc := range []struct{ name, label string }{
		{"fail2ban", "fail2ban"},
		{"sshguard", "sshguard"},
		{"crowdsec", "CrowdSec"},
	} {
		if out, err := exec.Command("systemctl", "is-active", svc.name).Output(); err == nil {
			if strings.TrimSpace(string(out)) == "active" {
				return true, svc.label + " is active"
			}
		}
	}
	return false, "No intrusion prevention service found — consider installing fail2ban or sshguard"
}

func checkNoEmptyPasswords() (bool, string) {
	b, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return true, "Cannot read /etc/shadow (requires root) — run webux as root to check this"
	}
	var empty []string
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) >= 2 && parts[1] == "" {
			empty = append(empty, parts[0])
		}
	}
	if len(empty) == 0 {
		return true, "No accounts with empty passwords"
	}
	return false, "Accounts with empty passwords: " + strings.Join(empty, ", ") + " — set a password or lock these accounts"
}
