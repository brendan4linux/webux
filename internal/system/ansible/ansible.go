// Package ansible discovers and runs Ansible playbooks.
// Zero external Go dependencies — uses the ansible binary on PATH.
package ansible

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ── Types ─────────────────────────────────────────────────────────────────

// Playbook represents a discovered Ansible playbook file.
type Playbook struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`         // filename without extension
	Plays       []Play   `json:"plays"`
	VarsFiles   []string `json:"vars_files"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`  // first play's name field
}

// Play is one play within a playbook.
type Play struct {
	Name  string `json:"name"`
	Hosts string `json:"hosts"`
	Vars  []Var  `json:"vars"`
}

// Var is a declared variable with its default value.
type Var struct {
	Name    string `json:"name"`
	Default string `json:"default"`
	Prompt  bool   `json:"prompt"`  // true if from vars_prompt
	Private bool   `json:"private"` // vars_prompt with private: yes (password)
}

// InventoryGroup is a parsed group from an inventory file.
type InventoryGroup struct {
	Name  string   `json:"name"`
	Hosts []string `json:"hosts"`
}

// RunOptions controls how a playbook is executed.
type RunOptions struct {
	PlaybookPath string            `json:"playbook_path"`
	Inventory    string            `json:"inventory"`    // path to inventory file
	Limit        string            `json:"limit"`        // --limit <pattern>
	Tags         string            `json:"tags"`         // --tags <tags>
	ExtraVars    map[string]string `json:"extra_vars"`   // --extra-vars
	Check        bool              `json:"check"`        // --check (dry run)
	Diff         bool              `json:"diff"`         // --diff
	Verbose      int               `json:"verbose"`      // 0-4 → -v through -vvvv
}

// ── Scanner ───────────────────────────────────────────────────────────────

// Scanner discovers and parses playbooks in a directory.
type Scanner struct{}

func NewScanner() *Scanner { return &Scanner{} }

// Scan walks dir looking for YAML files that look like playbooks.
func (s *Scanner) Scan(dir string) ([]Playbook, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("playbook directory %q not found", dir)
	}

	var playbooks []Playbook

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		// Skip role files and included task files
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}

		path := filepath.Join(dir, name)
		pb, err := s.parsePlaybook(path)
		if err != nil {
			continue // skip unparseable files silently
		}
		if len(pb.Plays) == 0 {
			continue // not a playbook (no plays)
		}
		playbooks = append(playbooks, pb)
	}

	return playbooks, nil
}

// parsePlaybook reads and parses a single playbook YAML file.
func (s *Scanner) parsePlaybook(path string) (Playbook, error) {
	pb := Playbook{
		Path: path,
		Name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return pb, err
	}

	// A playbook is a YAML list of plays
	var rawPlays []map[string]interface{}
	if err := yaml.Unmarshal(data, &rawPlays); err != nil {
		return pb, err
	}
	if len(rawPlays) == 0 {
		return pb, fmt.Errorf("empty or non-list YAML")
	}

	// Validate it looks like a playbook (first item has hosts: key)
	first := rawPlays[0]
	if _, ok := first["hosts"]; !ok {
		return pb, fmt.Errorf("no hosts key — not a playbook")
	}

	for _, raw := range rawPlays {
		play := Play{
			Name:  toString(raw["name"]),
			Hosts: toString(raw["hosts"]),
		}

		// Extract vars: block
		if vars, ok := raw["vars"]; ok {
			play.Vars = append(play.Vars, extractVars(vars, false)...)
		}

		// Extract vars_prompt: block
		if prompts, ok := raw["vars_prompt"]; ok {
			play.Vars = append(play.Vars, extractVarsPrompt(prompts)...)
		}

		pb.Plays = append(pb.Plays, play)
	}

	// Extract vars_files from first play
	if vf, ok := first["vars_files"]; ok {
		if list, ok := vf.([]interface{}); ok {
			for _, f := range list {
				pb.VarsFiles = append(pb.VarsFiles, toString(f))
			}
		}
	}

	// Use first play name as description
	if len(pb.Plays) > 0 && pb.Plays[0].Name != "" {
		pb.Description = pb.Plays[0].Name
	}

	// Collect all tags from tasks (best effort)
	pb.Tags = extractTags(rawPlays)

	return pb, nil
}

// extractVars parses a vars: block (map or list of maps).
func extractVars(raw interface{}, prompt bool) []Var {
	var vars []Var
	switch v := raw.(type) {
	case map[string]interface{}:
		for key, val := range v {
			vars = append(vars, Var{
				Name:    key,
				Default: fmt.Sprintf("%v", val),
				Prompt:  prompt,
			})
		}
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				for key, val := range m {
					vars = append(vars, Var{
						Name:    key,
						Default: fmt.Sprintf("%v", val),
						Prompt:  prompt,
					})
				}
			}
		}
	}
	return vars
}

// extractVarsPrompt parses a vars_prompt: block.
func extractVarsPrompt(raw interface{}) []Var {
	var vars []Var
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			switch p := item.(type) {
			case string:
				// Simple string form: vars_prompt: - varname
				vars = append(vars, Var{Name: p, Prompt: true})
			case map[string]interface{}:
				// Dict form: {name: x, prompt: "...", private: yes, default: ...}
				v := Var{
					Name:   toString(p["name"]),
					Prompt: true,
				}
				if def, ok := p["default"]; ok {
					v.Default = fmt.Sprintf("%v", def)
				}
				if priv, ok := p["private"]; ok {
					v.Private = priv == true || priv == "yes" || priv == "true"
				}
				if v.Name != "" {
					vars = append(vars, v)
				}
			}
		}
	}
	return vars
}

// extractTags does a best-effort collection of tag names from play tasks.
func extractTags(plays []map[string]interface{}) []string {
	seen := map[string]bool{}
	var tags []string
	for _, play := range plays {
		if tasks, ok := play["tasks"]; ok {
			if list, ok := tasks.([]interface{}); ok {
				for _, t := range list {
					if task, ok := t.(map[string]interface{}); ok {
						if rawTags, ok := task["tags"]; ok {
							switch tv := rawTags.(type) {
							case string:
								if !seen[tv] { seen[tv] = true; tags = append(tags, tv) }
							case []interface{}:
								for _, tag := range tv {
									s := toString(tag)
									if !seen[s] { seen[s] = true; tags = append(tags, s) }
								}
							}
						}
					}
				}
			}
		}
	}
	return tags
}

// ── Inventory ─────────────────────────────────────────────────────────────

// ParseInventory reads an Ansible INI or YAML inventory file and returns groups.
func ParseInventory(path string) ([]InventoryGroup, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read inventory: %w", err)
	}
	content := string(data)

	// Try YAML first
	var yamlInv map[string]interface{}
	if yaml.Unmarshal(data, &yamlInv) == nil && len(yamlInv) > 0 {
		return parseYAMLInventory(yamlInv), nil
	}

	// Fall back to INI format
	return parseINIInventory(content), nil
}

func parseINIInventory(content string) []InventoryGroup {
	var groups []InventoryGroup
	var current *InventoryGroup

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := line[1 : len(line)-1]
			if strings.Contains(name, ":") {
				continue // skip [group:children], [group:vars]
			}
			groups = append(groups, InventoryGroup{Name: name})
			current = &groups[len(groups)-1]
			continue
		}
		if current != nil && line != "" {
			// Host entry — take just the hostname, strip vars
			host := strings.Fields(line)[0]
			current.Hosts = append(current.Hosts, host)
		} else if current == nil && line != "" {
			// Ungrouped hosts
			found := false
			for i := range groups {
				if groups[i].Name == "ungrouped" {
					groups[i].Hosts = append(groups[i].Hosts, strings.Fields(line)[0])
					found = true
					break
				}
			}
			if !found {
				groups = append(groups, InventoryGroup{Name: "ungrouped", Hosts: []string{strings.Fields(line)[0]}})
				current = &groups[len(groups)-1]
			}
		}
	}
	return groups
}

func parseYAMLInventory(raw map[string]interface{}) []InventoryGroup {
	var groups []InventoryGroup
	for name, val := range raw {
		group := InventoryGroup{Name: name}
		if m, ok := val.(map[string]interface{}); ok {
			if hosts, ok := m["hosts"]; ok {
				switch h := hosts.(type) {
				case map[string]interface{}:
					for host := range h {
						group.Hosts = append(group.Hosts, host)
					}
				case []interface{}:
					for _, host := range h {
						group.Hosts = append(group.Hosts, toString(host))
					}
				}
			}
		}
		groups = append(groups, group)
	}
	return groups
}

// ── Runner ────────────────────────────────────────────────────────────────

// Runner executes Ansible playbooks.
type Runner struct{}

func NewRunner() *Runner { return &Runner{} }

// Installed returns true if ansible-playbook is on PATH.
func (r *Runner) Installed() bool {
	_, err := exec.LookPath("ansible-playbook")
	return err == nil
}

// Version returns the ansible version string.
func (r *Runner) Version() string {
	out, err := exec.Command("ansible", "--version").Output()
	if err != nil {
		return ""
	}
	lines := strings.SplitN(string(out), "\n", 2)
	return strings.TrimSpace(lines[0])
}

// Run executes a playbook and streams output lines to out channel.
// Returns the CLI equivalent command string.
func (r *Runner) Run(ctx context.Context, opts RunOptions, out chan<- string) (string, error) {
	args := buildArgs(opts)
	cliCmd := "ansible-playbook " + strings.Join(args, " ")

	cmd := exec.CommandContext(ctx, "ansible-playbook", args...)
	cmd.Env = append(os.Environ(), "ANSIBLE_FORCE_COLOR=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return cliCmd, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return cliCmd, err
	}

	if err := cmd.Start(); err != nil {
		return cliCmd, fmt.Errorf("start ansible-playbook: %w", err)
	}

	combined := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(combined)
	for scanner.Scan() {
		select {
		case out <- scanner.Text():
		case <-ctx.Done():
			cmd.Process.Kill()
			return cliCmd, ctx.Err()
		}
	}
	cmd.Wait()
	return cliCmd, nil
}

// buildArgs constructs the ansible-playbook argument list from RunOptions.
func buildArgs(opts RunOptions) []string {
	args := []string{opts.PlaybookPath}

	if opts.Inventory != "" {
		args = append(args, "-i", opts.Inventory)
	}
	if opts.Limit != "" {
		args = append(args, "--limit", opts.Limit)
	}
	if opts.Tags != "" {
		args = append(args, "--tags", opts.Tags)
	}
	if opts.Check {
		args = append(args, "--check")
	}
	if opts.Diff {
		args = append(args, "--diff")
	}
	if opts.Verbose > 0 {
		v := strings.Repeat("v", min(opts.Verbose, 4))
		args = append(args, "-"+v)
	}

	// Build --extra-vars JSON string
	if len(opts.ExtraVars) > 0 {
		var parts []string
		for k, v := range opts.ExtraVars {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		args = append(args, "--extra-vars", strings.Join(parts, " "))
	}

	return args
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
