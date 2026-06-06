// Package migration generates host migration templates by correlating
// open ports, enabled services, databases, webserver configs, users,
// cron jobs, and config management state into a structured report.
package migration

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/brendan4linux/webux/internal/system/network/ports"
)

// HostSnapshot is a point-in-time capture of everything that would need to
// be reproduced on a replacement host.
type HostSnapshot struct {
	CapturedAt    time.Time         `json:"captured_at"`
	Hostname      string            `json:"hostname"`
	Distro        string            `json:"distro"`
	KernelVersion string            `json:"kernel_version"`
	Arch          string            `json:"arch"`

	Ports         []ports.PortInfo  `json:"ports"`
	SystemdSockets []ports.SystemdSocketUnit `json:"systemd_sockets"`

	EnabledServices []ServiceSummary  `json:"enabled_services"`
	InstalledPkgs   []string          `json:"installed_packages"` // only those owning open ports
	Databases       []DatabaseSummary `json:"databases"`
	Webservers      []WebserverSummary `json:"webservers"`
	CronJobs        []CronJob         `json:"cron_jobs"`
	Users           []UserSummary     `json:"users"` // UID >= 1000
	EnvVars         []string          `json:"env_vars"` // from /etc/environment
	FirewallRules   []string          `json:"firewall_rules"`

	// Config management
	AnsibleInventories []string `json:"ansible_inventories"`
	PuppetFacts        map[string]interface{} `json:"puppet_facts,omitempty"`
}

// ServiceSummary is a minimal enabled systemd unit description.
type ServiceSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	ActiveState string `json:"active_state"`
}

// DatabaseSummary describes a detected running database instance.
type DatabaseSummary struct {
	Type    string `json:"type"`    // "mysql" | "postgres" | "sqlite"
	Port    uint16 `json:"port"`
	DataDir string `json:"data_dir"`
	Version string `json:"version"`
}

// WebserverSummary describes a detected webserver.
type WebserverSummary struct {
	Type       string   `json:"type"`
	ConfigPath string   `json:"config_path"`
	VHosts     []string `json:"vhosts"`
}

// CronJob represents a single crontab entry.
type CronJob struct {
	Owner    string `json:"owner"`  // "root", "system", or username
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Source   string `json:"source"` // file path or "crontab -l <user>"
}

// UserSummary is a non-system user account.
type UserSummary struct {
	Username string `json:"username"`
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
	Home     string `json:"home"`
	Shell    string `json:"shell"`
	Groups   []string `json:"groups"`
}

// Generator builds HostSnapshots and renders migration templates.
type Generator struct{}

// NewGenerator creates a Generator.
func NewGenerator() *Generator { return &Generator{} }

// Capture collects the full host snapshot. Individual sub-collectors
// degrade gracefully if a subsystem is not present.
func (g *Generator) Capture() (*HostSnapshot, error) {
	snap := &HostSnapshot{
		CapturedAt: time.Now().UTC(),
	}

	snap.Hostname, _ = os.Hostname()
	snap.KernelVersion = readFileTrimmed("/proc/sys/kernel/osrelease")
	snap.Arch = readFileTrimmed("/proc/sys/kernel/arch") // may be empty; fallback below
	if snap.Arch == "" {
		snap.Arch = detectArch()
	}
	snap.Distro = detectDistro()

	// Ports
	scanner := ports.NewScanner()
	listening, err := scanner.ScanListening()
	if err != nil {
		return nil, fmt.Errorf("port scan: %w", err)
	}
	snap.Ports = listening

	// Systemd sockets
	sdSockets, _ := ports.LoadSystemdSockets()
	snap.SystemdSockets = sdSockets
	ports.EnrichWithSystemdSockets(snap.Ports, sdSockets)

	// Enabled services
	snap.EnabledServices = collectEnabledServices()

	// Databases
	snap.Databases = detectDatabases(snap.Ports)

	// Webservers
	snap.Webservers = detectWebservers(snap.Ports)

	// Cron jobs
	snap.CronJobs = collectCronJobs()

	// Non-system users
	snap.Users = collectUsers()

	// /etc/environment
	snap.EnvVars = readEtcEnvironment()

	// Firewall rules (ufw preferred, fallback to iptables listing)
	snap.FirewallRules = collectFirewallRules()

	// Ansible inventories
	snap.AnsibleInventories = findAnsibleInventories()

	// Puppet facts
	snap.PuppetFacts = readPuppetFacts()

	return snap, nil
}

// RenderYAML returns a YAML migration plan.
func (g *Generator) RenderYAML(snap *HostSnapshot) (string, error) {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	// Produce clean YAML by re-marshaling through our template
	return string(b), nil // caller converts JSON → YAML if needed
}

// RenderMarkdown returns a human-readable migration checklist.
func (g *Generator) RenderMarkdown(snap *HostSnapshot) (string, error) {
	return renderTemplate("markdown", markdownTmpl, nil, snap)
}

// RenderAnsible generates a rough Ansible playbook skeleton.
func (g *Generator) RenderAnsible(snap *HostSnapshot) (string, error) {
	funcMap := template.FuncMap{
		"truncate": func(s string) string {
			if len(s) > 40 {
				return s[:40] + "..."
			}
			return s
		},
	}
	return renderTemplate("ansible", ansibleTmpl, funcMap, snap)
}

func renderTemplate(name, tmplStr string, funcMap template.FuncMap, data interface{}) (string, error) {
	t := template.New(name)
	if funcMap != nil {
		t = t.Funcs(funcMap)
	}
	t, err := t.Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// markdownTmpl is the migration checklist template.
// Stored as a var (not const) so we can build it without backtick-in-backtick issues.
var markdownTmpl = strings.Join([]string{
	`# Webux Migration Template`,
	`> Generated: {{.CapturedAt.Format "2006-01-02 15:04:05 UTC"}}`,
	`> Source host: **{{.Hostname}}** ({{.Distro}}, {{.Arch}}, kernel {{.KernelVersion}})`,
	``,
	`---`,
	``,
	`## 1. Open Ports & Sockets`,
	``,
	`| Port | Proto | State | Process | systemd socket |`,
	`|------|-------|-------|---------|----------------|`,
	`{{- range .Ports}}`,
	`| {{.LocalPort}} | {{.Proto}} | {{.State}} | {{.ProcessName}} | {{if .SystemdSocket}}{{.SystemdSocket}}{{else}}_none_{{end}} |`,
	`{{- end}}`,
	``,
	`---`,
	``,
	`## 2. Enabled Services to Recreate`,
	``,
	`{{range .EnabledServices -}}`,
	`- [ ] **{{.Name}}** — {{.Description}} *(was {{.ActiveState}})*`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 3. Databases`,
	``,
	`{{if .Databases}}`,
	`{{range .Databases -}}`,
	`- **{{.Type}}** on port {{.Port}}`,
	`  - Data directory: {{.DataDir}}`,
	`  - Version: {{.Version}}`,
	`  - Remember to dump and restore data, and replicate users/permissions.`,
	`{{end}}`,
	`{{else}}`,
	`_No databases detected._`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 4. Webservers`,
	``,
	`{{if .Webservers}}`,
	`{{range .Webservers -}}`,
	`- **{{.Type}}** — config at {{.ConfigPath}}`,
	`{{range .VHosts}}  - vhost: {{.}}`,
	`{{end}}`,
	`{{end}}`,
	`{{else}}`,
	`_No webservers detected._`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 5. Cron Jobs`,
	``,
	`{{if .CronJobs}}`,
	`| Owner | Schedule | Command |`,
	`|-------|----------|---------|`,
	`{{range .CronJobs -}}`,
	`| {{.Owner}} | {{.Schedule}} | {{.Command}} |`,
	`{{end}}`,
	`{{else}}`,
	`_No cron jobs found._`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 6. Non-System Users (UID >= 1000)`,
	``,
	`{{if .Users}}`,
	`{{range .Users -}}`,
	`- [ ] **{{.Username}}** (UID {{.UID}}) — home: {{.Home}}, shell: {{.Shell}}`,
	`  - Groups: {{range .Groups}}{{.}} {{end}}`,
	`{{end}}`,
	`{{else}}`,
	`_No non-system users._`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 7. Environment Variables (/etc/environment)`,
	``,
	`{{range .EnvVars}}    {{.}}`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 8. Firewall Rules`,
	``,
	`{{range .FirewallRules}}{{.}}`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## 9. Config Management`,
	``,
	`{{if .AnsibleInventories}}`,
	`**Ansible inventories found:**`,
	`{{range .AnsibleInventories -}}`,
	`- {{.}}`,
	`{{end}}`,
	`{{end}}`,
	``,
	`{{if .PuppetFacts}}`,
	`**Puppet facts captured** — see full snapshot JSON for details.`,
	`{{end}}`,
	``,
	`---`,
	``,
	`## Migration Checklist`,
	``,
	`- [ ] Provision new host with same OS/arch`,
	`- [ ] Replicate all enabled services (section 2)`,
	`- [ ] Open firewall ports: {{range .Ports}}{{.LocalPort}}/{{.Proto}} {{end}}`,
	`- [ ] Restore database dumps (section 3)`,
	`- [ ] Copy webserver configs (section 4)`,
	`- [ ] Restore cron jobs (section 5)`,
	`- [ ] Create users and groups (section 6)`,
	`- [ ] Apply environment variables (section 7)`,
	`- [ ] Apply firewall rules (section 8)`,
	`- [ ] Run Ansible playbooks / Puppet agent (section 9)`,
	`- [ ] Validate all ports listening: ss -tulpn`,
	`- [ ] Run smoke tests and cut over DNS/load balancer`,
}, "\n")

var ansibleTmpl = strings.Join([]string{
	`---`,
	`# Webux-generated migration playbook skeleton`,
	`# Source: {{.Hostname}} captured {{.CapturedAt.Format "2006-01-02"}}`,
	`# Review and complete all TODO sections before running.`,
	``,
	`- name: Migrate {{.Hostname}} workloads`,
	`  hosts: new_host`,
	`  become: true`,
	`  vars:`,
	`    source_hostname: "{{.Hostname}}"`,
	``,
	`  tasks:`,
	``,
	`    # --- Services ---`,
	`{{range .EnabledServices}}    - name: Enable {{.Name}}`,
	`      ansible.builtin.service:`,
	`        name: "{{.Name}}"`,
	`        state: started`,
	`        enabled: true`,
	`      # TODO: ensure package providing {{.Name}} is installed first`,
	`{{end}}`,
	``,
	`    # --- Users ---`,
	`{{range .Users}}    - name: Create user {{.Username}}`,
	`      ansible.builtin.user:`,
	`        name: "{{.Username}}"`,
	`        uid: {{.UID}}`,
	`        home: "{{.Home}}"`,
	`        shell: "{{.Shell}}"`,
	`        groups: "{{range .Groups}}{{.}},{{end}}"`,
	`        create_home: true`,
	`{{end}}`,
	``,
	`    # --- Firewall (ufw) ---`,
	`{{range .Ports}}    - name: Allow {{.LocalPort}}/{{.Proto}}`,
	`      community.general.ufw:`,
	`        rule: allow`,
	`        port: "{{.LocalPort}}"`,
	`        proto: "{{.Proto}}"`,
	`{{end}}`,
	``,
	`    # --- Cron Jobs ---`,
	`{{range .CronJobs}}    - name: Cron - {{.Owner}} - {{.Command | truncate}}`,
	`      ansible.builtin.cron:`,
	`        name: "migrated from {{$.Hostname}}"`,
	`        user: "{{.Owner}}"`,
	`        minute: "TODO"`,
	`        hour: "TODO"`,
	`        job: "{{.Command}}"`,
	`      # Original schedule: {{.Schedule}} - parse and fill in above fields`,
	`{{end}}`,
	``,
	`    # --- Environment Variables ---`,
	`    - name: Apply /etc/environment variables`,
	`      ansible.builtin.lineinfile:`,
	`        path: /etc/environment`,
	`        line: "{{ "{{" }} item {{ "}}" }}"`,
	`      loop:`,
	`{{range .EnvVars}}        - "{{.}}"`,
	`{{end}}`,
}, "\n")

// --- sub-collectors ---------------------------------------------------------

func collectEnabledServices() []ServiceSummary {
	// Try systemctl list-unit-files first; degrade gracefully if absent
	cmd := exec.Command("systemctl", "list-unit-files", "--type=service",
		"--state=enabled", "--no-pager", "--no-legend")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var services []ServiceSummary
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		services = append(services, ServiceSummary{
			Name:    fields[0],
			Enabled: fields[1] == "enabled",
		})
	}
	return services
}

func detectDatabases(openPorts []ports.PortInfo) []DatabaseSummary {
	knownDBPorts := map[uint16]string{
		3306:  "mysql",
		5432:  "postgres",
		27017: "mongodb",
		6379:  "redis",
		5984:  "couchdb",
		9200:  "elasticsearch",
	}

	dataDirs := map[string]string{
		"mysql":    "/var/lib/mysql",
		"postgres": "/var/lib/postgresql",
		"mongodb":  "/var/lib/mongodb",
	}

	var dbs []DatabaseSummary
	seen := make(map[uint16]bool)
	for _, p := range openPorts {
		if seen[p.LocalPort] {
			continue
		}
		if dbType, ok := knownDBPorts[p.LocalPort]; ok {
			seen[p.LocalPort] = true
			db := DatabaseSummary{
				Type: dbType,
				Port: p.LocalPort,
			}
			if d, ok := dataDirs[dbType]; ok {
				db.DataDir = d
			}
			db.Version = detectDBVersion(dbType)
			dbs = append(dbs, db)
		}
	}
	return dbs
}

func detectDBVersion(dbType string) string {
	cmds := map[string][]string{
		"mysql":    {"mysql", "--version"},
		"postgres": {"psql", "--version"},
		"redis":    {"redis-server", "--version"},
	}
	args, ok := cmds[dbType]
	if !ok {
		return "unknown"
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func detectWebservers(openPorts []ports.PortInfo) []WebserverSummary {
	type candidate struct {
		ports   []uint16
		configs []string
		name    string
	}
	candidates := []candidate{
		{[]uint16{80, 443}, []string{"/etc/nginx/nginx.conf"}, "nginx"},
		{[]uint16{80, 443, 8080}, []string{"/etc/apache2/apache2.conf", "/etc/httpd/httpd.conf"}, "apache"},
		{[]uint16{80, 443}, []string{"/etc/caddy/Caddyfile"}, "caddy"},
	}

	portSet := make(map[uint16]bool)
	for _, p := range openPorts {
		portSet[p.LocalPort] = true
	}

	var servers []WebserverSummary
	for _, c := range candidates {
		matched := false
		for _, p := range c.ports {
			if portSet[p] {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, cfg := range c.configs {
			if _, err := os.Stat(cfg); err == nil {
				ws := WebserverSummary{
					Type:       c.name,
					ConfigPath: cfg,
					VHosts:     findVHosts(c.name, cfg),
				}
				servers = append(servers, ws)
				break
			}
		}
	}
	return servers
}

func findVHosts(wsType, configPath string) []string {
	// Best-effort grep for ServerName / server_name directives
	dir := filepath.Dir(configPath)
	var args []string
	switch wsType {
	case "nginx":
		args = []string{"grep", "-rh", "server_name", dir}
	case "apache":
		args = []string{"grep", "-rh", "ServerName", dir}
	default:
		return nil
	}
	out, err := exec.Command(args[0], args[1:]...).Output()
	if err != nil {
		return nil
	}
	var hosts []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hosts = append(hosts, parts[1])
		}
	}
	return hosts
}


func collectCronJobs() []CronJob {
	var jobs []CronJob

	// System crontabs
	systemCronDirs := []string{"/etc/cron.d", "/etc/cron.daily", "/etc/cron.weekly", "/etc/cron.monthly"}
	for _, dir := range systemCronDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := dir + "/" + e.Name()
			cronJobs := parseCrontab(path, "system")
			jobs = append(jobs, cronJobs...)
		}
	}

	// /etc/crontab
	jobs = append(jobs, parseCrontab("/etc/crontab", "system")...)

	// Per-user crontabs from /var/spool/cron/crontabs/
	spoolDir := "/var/spool/cron/crontabs"
	entries, err := os.ReadDir(spoolDir)
	if err == nil {
		for _, e := range entries {
			path := spoolDir + "/" + e.Name()
			jobs = append(jobs, parseCrontab(path, e.Name())...)
		}
	}

	return jobs
}

func parseCrontab(path, owner string) []CronJob {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var jobs []CronJob
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Standard crontab: min hour dom mon dow [user] command
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		schedule := strings.Join(fields[:5], " ")
		command := strings.Join(fields[5:], " ")
		jobs = append(jobs, CronJob{
			Owner:    owner,
			Schedule: schedule,
			Command:  command,
			Source:   path,
		})
	}
	return jobs
}

func collectUsers() []UserSummary {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return nil
	}
	defer f.Close()

	var users []UserSummary
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}
		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		if uid < 1000 || uid == 65534 { // skip system accounts and nobody
			continue
		}
		gid, _ := strconv.Atoi(parts[3])
		users = append(users, UserSummary{
			Username: parts[0],
			UID:      uid,
			GID:      gid,
			Home:     parts[5],
			Shell:    parts[6],
			Groups:   getUserGroups(parts[0]),
		})
	}
	return users
}

func getUserGroups(username string) []string {
	out, err := exec.Command("groups", username).Output()
	if err != nil {
		return nil
	}
	// "username : group1 group2 group3"
	parts := strings.SplitN(string(out), ":", 2)
	if len(parts) != 2 {
		return nil
	}
	var groups []string
	for _, g := range strings.Fields(parts[1]) {
		groups = append(groups, strings.TrimSpace(g))
	}
	return groups
}

func readEtcEnvironment() []string {
	f, err := os.Open("/etc/environment")
	if err != nil {
		return nil
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

func collectFirewallRules() []string {
	// Try ufw first
	if out, err := exec.Command("ufw", "status", "numbered").Output(); err == nil {
		var rules []string
		for _, line := range strings.Split(string(out), "\n") {
			if strings.TrimSpace(line) != "" {
				rules = append(rules, line)
			}
		}
		return rules
	}
	// Fallback: nft list ruleset
	if out, err := exec.Command("nft", "list", "ruleset").Output(); err == nil {
		return strings.Split(string(out), "\n")
	}
	// Fallback: iptables-save
	if out, err := exec.Command("iptables-save").Output(); err == nil {
		return strings.Split(string(out), "\n")
	}
	return nil
}

func findAnsibleInventories() []string {
	candidates := []string{
		"/etc/webux/ansible/inventory",
		"/etc/ansible/hosts",
		"/etc/ansible/inventory",
	}
	var found []string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			found = append(found, c)
		}
	}
	return found
}

func readPuppetFacts() map[string]interface{} {
	out, err := exec.Command("facter", "-j").Output()
	if err != nil {
		return nil
	}
	var facts map[string]interface{}
	if err := json.Unmarshal(out, &facts); err != nil {
		return nil
	}
	return facts
}

func detectDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return "unknown"
}

func detectArch() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func readFileTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
