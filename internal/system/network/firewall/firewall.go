// Package firewall provides a unified interface over ufw, nftables, and iptables.
// Detection is automatic — whichever tool is active on the host is used.
package firewall

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Backend identifies which firewall tool is managing rules.
type Backend string

const (
	BackendUFW      Backend = "ufw"
	BackendNFTables Backend = "nftables"
	BackendIPTables Backend = "iptables"
	BackendNone     Backend = "none"
)

// Rule represents a single firewall rule in a normalised form.
type Rule struct {
	ID       string `json:"id"`        // rule number or nft handle
	Chain    string `json:"chain"`     // INPUT, OUTPUT, FORWARD, or nft chain name
	Table    string `json:"table"`     // filter, nat, mangle (iptables) or nft table
	Action   string `json:"action"`    // ACCEPT, DROP, REJECT, allow, deny
	Protocol string `json:"protocol"`  // tcp, udp, any
	SrcIP    string `json:"src_ip"`
	DstIP    string `json:"dst_ip"`
	Port     string `json:"port"`
	Comment  string `json:"comment"`
	Raw      string `json:"raw"` // original line as-is, for display
}

// Status is the overall firewall status.
type Status struct {
	Backend  Backend  `json:"backend"`
	Active   bool     `json:"active"`
	Rules    []Rule   `json:"rules"`
	RawLines []string `json:"raw_lines"` // unprocessed output for display
}

// Manager detects and operates the active firewall.
type Manager struct {
	backend Backend
}

// NewManager detects the active firewall backend.
func NewManager() *Manager {
	return &Manager{backend: detectBackend()}
}

func detectBackend() Backend {
	// UFW: check if active (not just installed)
	if commandExists("ufw") {
		out, err := exec.Command("ufw", "status").Output()
		if err == nil && strings.Contains(string(out), "Status: active") {
			return BackendUFW
		}
		// UFW installed but inactive — still use it as the interface
		if err == nil {
			return BackendUFW
		}
	}
	// nftables
	if commandExists("nft") {
		if out, err := exec.Command("nft", "list", "ruleset").Output(); err == nil && len(out) > 10 {
			return BackendNFTables
		}
	}
	// iptables fallback
	if commandExists("iptables") {
		return BackendIPTables
	}
	return BackendNone
}

// GetStatus returns the complete firewall status and all rules.
func (m *Manager) GetStatus() (*Status, error) {
	switch m.backend {
	case BackendUFW:
		return m.ufwStatus()
	case BackendNFTables:
		return m.nftStatus()
	case BackendIPTables:
		return m.iptablesStatus()
	default:
		return &Status{Backend: BackendNone, Active: false}, nil
	}
}

// AddRule adds a firewall rule. Returns the CLI equivalent.
func (m *Manager) AddRule(rule Rule) (string, error) {
	switch m.backend {
	case BackendUFW:
		return m.ufwAllow(rule)
	case BackendNFTables:
		return m.nftAdd(rule)
	case BackendIPTables:
		return m.iptablesAdd(rule)
	default:
		return "", fmt.Errorf("no firewall backend detected")
	}
}

// DeleteRule removes a rule by its ID. Returns the CLI equivalent.
func (m *Manager) DeleteRule(id string) (string, error) {
	switch m.backend {
	case BackendUFW:
		cmd := fmt.Sprintf("ufw delete %s", id)
		if err := exec.Command("ufw", "delete", id).Run(); err != nil {
			return "", fmt.Errorf("ufw delete: %w", err)
		}
		return cmd, nil
	case BackendNFTables:
		// id format: "table family chain handle"
		parts := strings.Fields(id)
		if len(parts) < 4 {
			return "", fmt.Errorf("invalid nft rule id: %s", id)
		}
		cmd := fmt.Sprintf("nft delete rule %s %s %s handle %s", parts[0], parts[1], parts[2], parts[3])
		args := []string{"delete", "rule", parts[0], parts[1], parts[2], "handle", parts[3]}
		if err := exec.Command("nft", args...).Run(); err != nil {
			return "", fmt.Errorf("nft delete: %w", err)
		}
		return cmd, nil
	case BackendIPTables:
		cmd := fmt.Sprintf("iptables -D INPUT %s", id)
		if err := exec.Command("iptables", "-D", "INPUT", id).Run(); err != nil {
			return "", fmt.Errorf("iptables delete: %w", err)
		}
		return cmd, nil
	default:
		return "", fmt.Errorf("no firewall backend")
	}
}

// Enable activates the firewall. Returns the CLI equivalent.
func (m *Manager) Enable() (string, error) {
	switch m.backend {
	case BackendUFW:
		if err := exec.Command("ufw", "--force", "enable").Run(); err != nil {
			return "", err
		}
		return "ufw --force enable", nil
	default:
		return "", fmt.Errorf("enable not supported for %s via webux — manage manually", m.backend)
	}
}

// Disable deactivates the firewall. Returns the CLI equivalent.
func (m *Manager) Disable() (string, error) {
	switch m.backend {
	case BackendUFW:
		if err := exec.Command("ufw", "disable").Run(); err != nil {
			return "", err
		}
		return "ufw disable", nil
	default:
		return "", fmt.Errorf("disable not supported for %s via webux — manage manually", m.backend)
	}
}

// Backend returns the detected backend name.
func (m *Manager) Backend() Backend { return m.backend }

// --- UFW --------------------------------------------------------------------

func (m *Manager) ufwStatus() (*Status, error) {
	out, err := exec.Command("ufw", "status", "verbose").Output()
	if err != nil {
		// ufw might not be installed or accessible
		return &Status{Backend: BackendUFW, Active: false}, nil
	}

	lines := strings.Split(string(out), "\n")
	status := &Status{Backend: BackendUFW, RawLines: lines}

	for _, line := range lines {
		if strings.HasPrefix(line, "Status:") {
			status.Active = strings.Contains(line, "active")
		}
	}

	// Get numbered rules for easy deletion
	numberedOut, _ := exec.Command("ufw", "status", "numbered").Output()
	status.Rules = parseUFWNumbered(string(numberedOut))
	status.RawLines = lines

	return status, nil
}

func parseUFWNumbered(out string) []Rule {
	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		// Lines look like: "[ 1] 22/tcp                     ALLOW IN    Anywhere"
		if !strings.HasPrefix(strings.TrimSpace(line), "[") {
			continue
		}
		// Extract rule number
		trimmed := strings.TrimSpace(line)
		endBracket := strings.Index(trimmed, "]")
		if endBracket < 0 {
			continue
		}
		num := strings.TrimSpace(trimmed[1:endBracket])
		rest := strings.TrimSpace(trimmed[endBracket+1:])

		fields := strings.Fields(rest)
		rule := Rule{
			ID:  num,
			Raw: line,
		}
		if len(fields) >= 1 {
			// Parse port/protocol
			portProto := fields[0]
			if strings.Contains(portProto, "/") {
				parts := strings.SplitN(portProto, "/", 2)
				rule.Port = parts[0]
				rule.Protocol = parts[1]
			} else {
				rule.Port = portProto
			}
		}
		if len(fields) >= 2 {
			rule.Action = fields[1]
		}
		if len(fields) >= 4 {
			rule.SrcIP = fields[3]
		}
		rules = append(rules, rule)
	}
	return rules
}

func (m *Manager) ufwAllow(rule Rule) (string, error) {
	var args []string
	action := "allow"
	if strings.ToLower(rule.Action) == "deny" || strings.ToLower(rule.Action) == "drop" {
		action = "deny"
	}

	args = append(args, action)
	if rule.Protocol != "" && rule.Protocol != "any" {
		if rule.Port != "" {
			args = append(args, rule.Port+"/"+rule.Protocol)
		} else {
			args = append(args, "proto", rule.Protocol)
		}
	} else if rule.Port != "" {
		args = append(args, rule.Port)
	}

	if rule.SrcIP != "" {
		args = append([]string{"from", rule.SrcIP}, args...)
	}

	if rule.Comment != "" {
		args = append(args, "comment", rule.Comment)
	}

	cmd := "ufw " + strings.Join(args, " ")
	fullArgs := append([]string{"ufw"}, args...)
	if err := exec.Command(fullArgs[0], fullArgs[1:]...).Run(); err != nil {
		return "", fmt.Errorf("ufw: %w", err)
	}
	return cmd, nil
}

// --- nftables ---------------------------------------------------------------

func (m *Manager) nftStatus() (*Status, error) {
	out, err := exec.Command("nft", "-j", "list", "ruleset").Output()
	if err != nil {
		// Try without JSON flag (older nft)
		out, err = exec.Command("nft", "list", "ruleset").Output()
		if err != nil {
			return &Status{Backend: BackendNFTables, Active: false}, nil
		}
	}

	lines := strings.Split(string(out), "\n")
	rules := parseNFTRules(lines)

	return &Status{
		Backend:  BackendNFTables,
		Active:   len(rules) > 0,
		Rules:    rules,
		RawLines: lines,
	}, nil
}

func parseNFTRules(lines []string) []Rule {
	var rules []Rule
	var currentTable, currentChain, currentFamily string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "table") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
				currentFamily = parts[1]
				currentTable = parts[2]
				currentChain = ""
			}
		} else if strings.HasPrefix(trimmed, "chain") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				currentChain = parts[1]
			}
		} else if strings.Contains(trimmed, "accept") || strings.Contains(trimmed, "drop") ||
			strings.Contains(trimmed, "reject") || strings.Contains(trimmed, "return") {
			action := "accept"
			if strings.Contains(trimmed, "drop") {
				action = "drop"
			} else if strings.Contains(trimmed, "reject") {
				action = "reject"
			}

			rule := Rule{
				ID:     fmt.Sprintf("%s %s %s %d", currentTable, currentFamily, currentChain, i),
				Table:  currentTable,
				Chain:  currentChain,
				Action: action,
				Raw:    line,
			}

			// Best-effort port extraction
			if idx := strings.Index(trimmed, "dport"); idx >= 0 {
				rest := strings.Fields(trimmed[idx:])
				if len(rest) >= 2 {
					rule.Port = rest[1]
				}
			}
			if strings.Contains(trimmed, "tcp") {
				rule.Protocol = "tcp"
			} else if strings.Contains(trimmed, "udp") {
				rule.Protocol = "udp"
			}

			rules = append(rules, rule)
		}
	}
	return rules
}

var (
	nftIdentRe         = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	nftPortRe          = regexp.MustCompile(`^\d{1,5}(-\d{1,5})?$`)
	nftAllowedActions  = map[string]bool{"accept": true, "drop": true, "reject": true}
	nftAllowedProtos   = map[string]bool{"tcp": true, "udp": true, "ip": true, "ip6": true, "inet": true}
)

func (m *Manager) nftAdd(rule Rule) (string, error) {
	proto := strings.ToLower(rule.Protocol)
	if proto == "" {
		proto = "tcp"
	}
	if !nftAllowedProtos[proto] {
		return "", fmt.Errorf("nft: invalid protocol %q", proto)
	}

	table := rule.Table
	if table == "" {
		table = "filter"
	}
	tableFamily := "inet"
	// table may be "inet filter" — split if so
	if parts := strings.Fields(table); len(parts) == 2 {
		tableFamily = parts[0]
		table = parts[1]
	}
	if !nftIdentRe.MatchString(tableFamily) {
		return "", fmt.Errorf("nft: invalid table family %q", tableFamily)
	}
	if !nftIdentRe.MatchString(table) {
		return "", fmt.Errorf("nft: invalid table name %q", table)
	}

	chain := rule.Chain
	if chain == "" {
		chain = "input"
	}
	if !nftIdentRe.MatchString(chain) {
		return "", fmt.Errorf("nft: invalid chain name %q", chain)
	}

	action := strings.ToLower(rule.Action)
	if action == "" {
		action = "accept"
	}
	if !nftAllowedActions[action] {
		return "", fmt.Errorf("nft: invalid action %q", action)
	}

	if rule.Port != "" && !nftPortRe.MatchString(rule.Port) {
		return "", fmt.Errorf("nft: invalid port %q", rule.Port)
	}

	// Build structured argv — no shell involved
	args := []string{"add", "rule", tableFamily, table, chain}
	if rule.Port != "" {
		args = append(args, proto, "dport", rule.Port)
	}
	args = append(args, action)

	cmd := "nft " + strings.Join(args, " ")
	if err := exec.Command("nft", args...).Run(); err != nil {
		return "", fmt.Errorf("nft add rule: %w", err)
	}
	return cmd, nil
}

// --- iptables ---------------------------------------------------------------

func (m *Manager) iptablesStatus() (*Status, error) {
	out, err := exec.Command("iptables", "-L", "-n", "--line-numbers", "-v").Output()
	if err != nil {
		return &Status{Backend: BackendIPTables, Active: false}, nil
	}

	lines := strings.Split(string(out), "\n")
	rules := parseIPTablesLines(lines)

	return &Status{
		Backend:  BackendIPTables,
		Active:   true,
		Rules:    rules,
		RawLines: lines,
	}, nil
}

func parseIPTablesLines(lines []string) []Rule {
	var rules []Rule
	var currentChain string

	for _, line := range lines {
		if strings.HasPrefix(line, "Chain ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				currentChain = fields[1]
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		// num pkts bytes target prot opt in out source destination
		num := fields[0]
		if _, err := fmt.Sscanf(num, "%d", new(int)); err != nil {
			continue
		}
		rules = append(rules, Rule{
			ID:       num,
			Chain:    currentChain,
			Table:    "filter",
			Action:   fields[3],
			Protocol: fields[4],
			SrcIP:    fields[8],
			DstIP:    fields[9],
			Raw:      line,
		})
	}
	return rules
}

func (m *Manager) iptablesAdd(rule Rule) (string, error) {
	chain := rule.Chain
	if chain == "" {
		chain = "INPUT"
	}
	action := strings.ToUpper(rule.Action)
	if action == "" {
		action = "ACCEPT"
	}

	args := []string{"-A", chain}
	if rule.Protocol != "" && rule.Protocol != "any" {
		args = append(args, "-p", rule.Protocol)
	}
	if rule.SrcIP != "" {
		args = append(args, "-s", rule.SrcIP)
	}
	if rule.Port != "" {
		args = append(args, "--dport", rule.Port)
	}
	if rule.Comment != "" {
		args = append(args, "-m", "comment", "--comment", rule.Comment)
	}
	args = append(args, "-j", action)

	cmd := "iptables " + strings.Join(args, " ")
	if err := exec.Command("iptables", args...).Run(); err != nil {
		return "", fmt.Errorf("iptables: %w", err)
	}
	return cmd, nil
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// CLIEquivalentStatus returns the shell command to view firewall status.
func CLIEquivalentStatus(backend Backend) string {
	switch backend {
	case BackendUFW:
		return "ufw status verbose"
	case BackendNFTables:
		return "nft list ruleset"
	case BackendIPTables:
		return "iptables -L -n --line-numbers -v"
	default:
		return "# no firewall detected"
	}
}

// readFile is a helper used in tests.
func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}
