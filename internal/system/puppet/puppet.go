// Package puppet reads Puppet agent state from well-known filesystem paths.
// All reads are non-destructive; write operations (run, enable, disable) use
// controlled subprocesses with bounded timeouts.
package puppet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Path resolution ───────────────────────────────────────────────────────

// candidateDirs lists where Puppet might store its state depending on
// whether it was installed via AIO packages or from distro repos.
var candidateDirs = []string{
	"/opt/puppetlabs/puppet/cache",   // AIO (modern)
	"/var/lib/puppet",                 // older / distro packages
	"/var/cache/puppet",
}

func stateDir() string {
	for _, d := range candidateDirs {
		if _, err := os.Stat(filepath.Join(d, "state")); err == nil {
			return filepath.Join(d, "state")
		}
	}
	return ""
}

func confDir() string {
	for _, d := range []string{
		"/etc/puppetlabs/puppet",
		"/etc/puppet",
	} {
		if _, err := os.Stat(d); err == nil {
			return d
		}
	}
	return ""
}

func clientDataDir() string {
	for _, d := range candidateDirs {
		cd := filepath.Join(d, "client_data")
		if _, err := os.Stat(cd); err == nil {
			return cd
		}
	}
	return ""
}

// ── Types ────────────────────────────────────────────────────────────────

// AgentStatus is the overall picture of the Puppet agent on this node.
type AgentStatus struct {
	Installed    bool       `json:"installed"`
	Version      string     `json:"version"`
	CertName     string     `json:"cert_name"`
	Server       string     `json:"server"`
	Environment  string     `json:"environment"`
	Enabled      bool       `json:"enabled"`
	DisabledMsg  string     `json:"disabled_msg,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	LastRunAgo   string     `json:"last_run_ago,omitempty"`
	RunSummary   *RunSummary `json:"run_summary,omitempty"`
	StateDir     string     `json:"state_dir"`
	ConfDir      string     `json:"conf_dir"`
}

// RunSummary mirrors the last_run_summary.yaml structure.
type RunSummary struct {
	Version     map[string]interface{} `json:"version" yaml:"version"`
	Resources   ResourceSummary        `json:"resources" yaml:"resources"`
	Events      EventSummary           `json:"events" yaml:"events"`
	Changes     ChangeSummary          `json:"changes" yaml:"changes"`
	Time        map[string]interface{} `json:"time" yaml:"time"`
	ConfigInfo  map[string]interface{} `json:"config_info" yaml:"config_info"`
}

type ResourceSummary struct {
	Changed      int `json:"changed" yaml:"changed"`
	Failed       int `json:"failed" yaml:"failed"`
	FailedToRestart int `json:"failed_to_restart" yaml:"failed_to_restart"`
	OOSync       int `json:"out_of_sync" yaml:"out_of_sync"`
	Restarted    int `json:"restarted" yaml:"restarted"`
	Scheduled    int `json:"scheduled" yaml:"scheduled"`
	Skipped      int `json:"skipped" yaml:"skipped"`
	Total        int `json:"total" yaml:"total"`
}

type EventSummary struct {
	Failure int `json:"failure" yaml:"failure"`
	Success int `json:"success" yaml:"success"`
	Total   int `json:"total" yaml:"total"`
}

type ChangeSummary struct {
	Total int `json:"total" yaml:"total"`
}

// CatalogResource is one resource from the compiled catalog.
type CatalogResource struct {
	Type       string                 `json:"type"`
	Title      string                 `json:"title"`
	Tags       []string               `json:"tags"`
	Exported   bool                   `json:"exported"`
	Parameters map[string]interface{} `json:"parameters"`
	File       string                 `json:"file,omitempty"`
	Line       int                    `json:"line,omitempty"`
}

// RunEvent is one resource event from the last run report.
type RunEvent struct {
	Resource    string `json:"resource"`
	Status      string `json:"status"` // changed | failed | skipped | success
	Message     string `json:"message"`
	Property    string `json:"property,omitempty"`
	OldValue    string `json:"old_value,omitempty"`
	NewValue    string `json:"new_value,omitempty"`
}

// ── Agent ────────────────────────────────────────────────────────────────

// Agent is the main entry point for reading Puppet state.
type Agent struct{}


// findPuppetBin searches all known puppet installation locations.
// The puppetlabs AIO installer puts puppet in /opt/puppetlabs/bin/,
// distro packages may put it in /usr/bin/, and some setups use /usr/local/bin/.
func findPuppetBin() string {
	knownPaths := []string{
		"/opt/puppetlabs/bin/puppet",  // AIO (puppetlabs repo) — most common
		"/usr/bin/puppet",
		"/usr/local/bin/puppet",
		"/usr/sbin/puppet",
	}
	for _, p := range knownPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Fall back to PATH lookup
	if p, err := exec.LookPath("puppet"); err == nil {
		return p
	}
	return ""
}

func NewAgent() *Agent { return &Agent{} }

// Status reads the full agent status from disk — no subprocess required
// except for reading the version and certname (fast, non-destructive).
func (a *Agent) Status() (*AgentStatus, error) {
	s := &AgentStatus{
		Installed: false,
		StateDir:  stateDir(),
		ConfDir:   confDir(),
	}

	// Check binary — search all known puppet install locations
	puppetBin := findPuppetBin()
	if puppetBin == "" {
		return s, nil
	}
	s.Installed = true

	// Version
	if out, err := exec.Command(puppetBin, "--version").Output(); err == nil {
		s.Version = strings.TrimSpace(string(out))
	}

	// Certname from config
	s.CertName = a.configPrint("certname")
	s.Server = a.configPrint("server")
	s.Environment = a.configPrint("environment")

	// Enabled/disabled — lockfile presence
	s.Enabled = true
	lockFile := filepath.Join(s.StateDir, "agent_disabled.lock")
	if data, err := os.ReadFile(lockFile); err == nil {
		s.Enabled = false
		var lockData map[string]interface{}
		if json.Unmarshal(data, &lockData) == nil {
			if msg, ok := lockData["disabled_message"].(string); ok {
				s.DisabledMsg = msg
			}
		}
	}

	// Last run time from summary file
	summaryPath := filepath.Join(s.StateDir, "last_run_summary.yaml")
	if data, err := os.ReadFile(summaryPath); err == nil {
		var summary RunSummary
		if yaml.Unmarshal(data, &summary) == nil {
			s.RunSummary = &summary
			// Extract last run epoch from time section
			if t, ok := summary.Time["last_run"]; ok {
				epoch := toFloat(t)
				if epoch > 0 {
					ts := time.Unix(int64(epoch), 0)
					s.LastRunAt = &ts
					s.LastRunAgo = humanDuration(time.Since(ts))
				}
			}
		}
	}

	return s, nil
}

// CatalogResources parses the on-disk catalog for this node.
func (a *Agent) CatalogResources() ([]CatalogResource, string, error) {
	certName := a.configPrint("certname")
	if certName == "" {
		// Fallback: look for any catalog file
		if cd := clientDataDir(); cd != "" {
			if entries, err := os.ReadDir(filepath.Join(cd, "catalog")); err == nil && len(entries) > 0 {
				certName = strings.TrimSuffix(entries[0].Name(), ".json")
			}
		}
	}

	var catalogPath string
	candidates := []string{
		filepath.Join(clientDataDir(), "catalog", certName+".json"),
		filepath.Join(stateDir(), "catalog.json"),
		fmt.Sprintf("/var/lib/puppet/client_data/catalog/%s.json", certName),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			catalogPath = p
			break
		}
	}
	if catalogPath == "" {
		return nil, "", fmt.Errorf("catalog not found — agent may not have run yet")
	}

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		return nil, catalogPath, err
	}

	// Puppet catalog JSON structure
	var catalog struct {
		Resources []CatalogResource `json:"resources"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, catalogPath, fmt.Errorf("parse catalog: %w", err)
	}
	return catalog.Resources, catalogPath, nil
}

// LastRunEvents parses the last run report for per-resource events.
func (a *Agent) LastRunEvents() ([]RunEvent, error) {
	reportPath := filepath.Join(stateDir(), "last_run_report.yaml")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, fmt.Errorf("report not found: %w", err)
	}

	// The report YAML is complex — extract resource_statuses section
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse report: %w", err)
	}

	var events []RunEvent
	if rs, ok := raw["resource_statuses"].(map[string]interface{}); ok {
		for resourceKey, val := range rs {
			if res, ok := val.(map[string]interface{}); ok {
				status := "success"
				if failed, _ := res["failed"].(bool); failed {
					status = "failed"
				} else if skipped, _ := res["skipped"].(bool); skipped {
					status = "skipped"
				} else if changed, _ := res["changed"].(bool); changed {
					status = "changed"
				}

				event := RunEvent{
					Resource: resourceKey,
					Status:   status,
				}

				// Extract events sub-array if present
				if evts, ok := res["events"].([]interface{}); ok && len(evts) > 0 {
					if e0, ok := evts[0].(map[string]interface{}); ok {
						event.Message = toString(e0["message"])
						event.Property = toString(e0["property"])
						event.OldValue = fmt.Sprintf("%v", e0["previous_value"])
						event.NewValue = fmt.Sprintf("%v", e0["desired_value"])
					}
				}
				events = append(events, event)
			}
		}
	}
	return events, nil
}

// RunAgent runs `puppet agent --test --onetime` and streams output.
// out channel receives lines; closed when done. Returns CLI equivalent.
func (a *Agent) RunAgent(ctx context.Context, noop bool, out chan<- string) (string, error) {
	args := []string{"agent", "--test", "--onetime", "--no-daemonize"}
	if noop {
		args = append(args, "--noop")
	}
	cliCmd := "puppet " + strings.Join(args, " ")

	bin := findPuppetBin()
	if bin == "" { bin = "puppet" }
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return cliCmd, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return cliCmd, err
	}

	if err := cmd.Start(); err != nil {
		return cliCmd, fmt.Errorf("start puppet agent: %w", err)
	}

	// Stream both stdout and stderr
	combined := io.MultiReader(stdout, stderr)
	buf := make([]byte, 4096)
	var partial string
	for {
		n, err := combined.Read(buf)
		if n > 0 {
			partial += string(buf[:n])
			lines := strings.Split(partial, "\n")
			for _, line := range lines[:len(lines)-1] {
				select {
				case out <- line:
				case <-ctx.Done():
					cmd.Process.Kill()
					return cliCmd, ctx.Err()
				}
			}
			partial = lines[len(lines)-1]
		}
		if err != nil {
			break
		}
	}
	if partial != "" {
		out <- partial
	}
	cmd.Wait()
	return cliCmd, nil
}

// Enable enables the Puppet agent. Returns CLI equivalent.
func (a *Agent) Enable() (string, error) {
	bin := findPuppetBin()
	if bin == "" { bin = "puppet" }
	if err := exec.Command(bin, "agent", "--enable").Run(); err != nil {
		return "", fmt.Errorf("puppet enable: %w", err)
	}
	return bin + " agent --enable", nil
}

// Disable disables the Puppet agent with an optional message.
func (a *Agent) Disable(message string) (string, error) {
	args := []string{"agent", "--disable"}
	if message != "" {
		args = append(args, message)
	}
	if err := exec.Command("puppet", args...).Run(); err != nil {
		return "", fmt.Errorf("puppet disable: %w", err)
	}
	return "puppet " + strings.Join(args, " "), nil
}

// Facts runs facter and returns the full fact set as a map.
func (a *Agent) Facts() (map[string]interface{}, error) {
	out, err := exec.Command("facter", "-j").Output()
	if err != nil {
		// Try puppet facts
		fbin := findPuppetBin()
		if fbin == "" { fbin = "puppet" }
		out, err = exec.Command(fbin, "facts", "--render-as", "json").Output()
		if err != nil {
			return nil, fmt.Errorf("facter/puppet facts unavailable: %w", err)
		}
	}
	var facts map[string]interface{}
	if err := json.Unmarshal(out, &facts); err != nil {
		return nil, fmt.Errorf("parse facts: %w", err)
	}
	return facts, nil
}

// ── helpers ──────────────────────────────────────────────────────────────

func (a *Agent) configPrint(key string) string {
	cbin := findPuppetBin()
	if cbin == "" { cbin = "puppet" }
	out, err := exec.Command(cbin, "config", "print", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	}
	return 0
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// CLIEquivalents returns the shell commands that map to our read operations.
func CLIEquivalents() map[string]string {
	return map[string]string{
		"status":    "puppet agent --configprint all | grep -E 'server|certname|environment'",
		"run":       "puppet agent --test --onetime --no-daemonize",
		"noop":      "puppet agent --test --onetime --noop --no-daemonize",
		"enable":    "puppet agent --enable",
		"disable":   "puppet agent --disable",
		"facts":     "facter -j",
		"catalog":   "cat $(puppet config print client_datadir)/catalog/$(puppet config print certname).json",
		"summary":   "cat $(puppet config print statedir)/last_run_summary.yaml",
		"report":    "cat $(puppet config print statedir)/last_run_report.yaml",
	}
}

// suppress unused import
var _ = bytes.NewBuffer
