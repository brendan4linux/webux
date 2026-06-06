// Package webservers detects and manages Nginx, Apache, and Caddy.
package webservers

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Type identifies the webserver engine.
type Type string

const (
	TypeNginx  Type = "nginx"
	TypeApache Type = "apache"
	TypeCaddy  Type = "caddy"
)

// VHost is a virtual host / server block.
type VHost struct {
	ServerName string   `json:"server_name"`
	Aliases    []string `json:"aliases"`
	Root       string   `json:"root"`
	Port       int      `json:"port"`
	SSL        bool     `json:"ssl"`
	ConfigFile string   `json:"config_file"`
	Enabled    bool     `json:"enabled"`
}

// Server is a detected webserver instance.
type Server struct {
	Type       Type     `json:"type"`
	Version    string   `json:"version"`
	ConfigPath string   `json:"config_path"`
	PID        int      `json:"pid"`
	Running    bool     `json:"running"`
	VHosts     []VHost  `json:"vhosts"`
}

// Manager handles all detected webservers.
type Manager struct {
	servers []Server
}

// NewManager detects all running webservers and returns a Manager.
func NewManager() *Manager {
	m := &Manager{}
	if s := detectNginx(); s != nil {
		m.servers = append(m.servers, *s)
	}
	if s := detectApache(); s != nil {
		m.servers = append(m.servers, *s)
	}
	if s := detectCaddy(); s != nil {
		m.servers = append(m.servers, *s)
	}
	return m
}

// List returns all detected webservers.
func (m *Manager) List() []Server { return m.servers }

// ReadConfig returns the raw config file content for a server.
func (m *Manager) ReadConfig(serverType Type) (string, string, error) {
	for _, s := range m.servers {
		if s.Type != serverType {
			continue
		}
		data, err := os.ReadFile(s.ConfigPath)
		if err != nil {
			return "", s.ConfigPath, fmt.Errorf("read config: %w", err)
		}
		return string(data), s.ConfigPath, nil
	}
	return "", "", fmt.Errorf("webserver %q not detected", serverType)
}

// WriteConfig writes new content to a server's config file and tests it.
// Returns CLI equivalent.
func (m *Manager) WriteConfig(serverType Type, content string) (string, error) {
	for _, s := range m.servers {
		if s.Type != serverType {
			continue
		}
		// Write to temp file and test first
		tmp := s.ConfigPath + ".webux.tmp"
		if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("write temp config: %w", err)
		}
		// Test the config
		if err := testConfig(serverType, tmp); err != nil {
			os.Remove(tmp)
			return "", fmt.Errorf("config test failed: %w", err)
		}
		// Swap in
		if err := os.Rename(tmp, s.ConfigPath); err != nil {
			os.Remove(tmp)
			return "", fmt.Errorf("rename config: %w", err)
		}
		return fmt.Sprintf("# Config saved to %s", s.ConfigPath), nil
	}
	return "", fmt.Errorf("webserver %q not detected", serverType)
}

// Reload sends a reload signal to a running webserver. Returns CLI equivalent.
func (m *Manager) Reload(serverType Type) (string, error) {
	switch serverType {
	case TypeNginx:
		if err := exec.Command("nginx", "-s", "reload").Run(); err != nil {
			return "", fmt.Errorf("nginx reload: %w", err)
		}
		return "nginx -s reload", nil
	case TypeApache:
		// Try apachectl then apache2ctl
		for _, bin := range []string{"apachectl", "apache2ctl"} {
			if err := exec.Command(bin, "graceful").Run(); err == nil {
				return bin + " graceful", nil
			}
		}
		return "", fmt.Errorf("apache reload failed")
	case TypeCaddy:
		if err := exec.Command("caddy", "reload", "--config", "/etc/caddy/Caddyfile").Run(); err != nil {
			return "", fmt.Errorf("caddy reload: %w", err)
		}
		return "caddy reload --config /etc/caddy/Caddyfile", nil
	}
	return "", fmt.Errorf("unknown webserver type: %s", serverType)
}

// TestConfig runs a config syntax check. Returns CLI equivalent and any errors.
func (m *Manager) TestConfig(serverType Type) (string, error) {
	switch serverType {
	case TypeNginx:
		out, err := exec.Command("nginx", "-t").CombinedOutput()
		if err != nil {
			return "nginx -t", fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return "nginx -t", nil
	case TypeApache:
		for _, bin := range []string{"apachectl", "apache2ctl"} {
			out, err := exec.Command(bin, "configtest").CombinedOutput()
			if err != nil {
				return bin + " configtest", fmt.Errorf("%s", strings.TrimSpace(string(out)))
			}
			return bin + " configtest", nil
		}
		return "", fmt.Errorf("apachectl not found")
	case TypeCaddy:
		out, err := exec.Command("caddy", "validate", "--config", "/etc/caddy/Caddyfile").CombinedOutput()
		if err != nil {
			return "caddy validate", fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return "caddy validate --config /etc/caddy/Caddyfile", nil
	}
	return "", fmt.Errorf("unknown type: %s", serverType)
}

// ListSites returns the enabled/disabled sites for Nginx or Apache.
func (m *Manager) ListSites(serverType Type) ([]VHost, error) {
	for _, s := range m.servers {
		if s.Type == serverType {
			return s.VHosts, nil
		}
	}
	return nil, fmt.Errorf("webserver %q not detected", serverType)
}

// EnableSite runs a2ensite or creates an nginx symlink. Returns CLI equivalent.
func (m *Manager) EnableSite(serverType Type, site string) (string, error) {
	switch serverType {
	case TypeApache:
		if err := exec.Command("a2ensite", site).Run(); err != nil {
			return "", fmt.Errorf("a2ensite: %w", err)
		}
		return "a2ensite " + site, nil
	case TypeNginx:
		src := "/etc/nginx/sites-available/" + site
		dst := "/etc/nginx/sites-enabled/" + site
		if err := os.Symlink(src, dst); err != nil {
			return "", fmt.Errorf("symlink: %w", err)
		}
		return fmt.Sprintf("ln -s %s %s", src, dst), nil
	}
	return "", fmt.Errorf("enable site not supported for %s", serverType)
}

// DisableSite runs a2dissite or removes an nginx symlink. Returns CLI equivalent.
func (m *Manager) DisableSite(serverType Type, site string) (string, error) {
	switch serverType {
	case TypeApache:
		if err := exec.Command("a2dissite", site).Run(); err != nil {
			return "", fmt.Errorf("a2dissite: %w", err)
		}
		return "a2dissite " + site, nil
	case TypeNginx:
		dst := "/etc/nginx/sites-enabled/" + site
		if err := os.Remove(dst); err != nil {
			return "", fmt.Errorf("remove symlink: %w", err)
		}
		return "rm " + dst, nil
	}
	return "", fmt.Errorf("disable site not supported for %s", serverType)
}

// --- detection helpers ------------------------------------------------------

func detectNginx() *Server {
	if _, err := exec.LookPath("nginx"); err != nil {
		return nil
	}
	s := &Server{
		Type:       TypeNginx,
		Version:    runOutput("nginx", "-v"),
		ConfigPath: findFirst("/etc/nginx/nginx.conf"),
		Running:    processRunning("nginx"),
	}
	if s.ConfigPath == "" {
		return nil
	}
	s.VHosts = parseNginxVHosts()
	return s
}

func detectApache() *Server {
	for _, bin := range []string{"apache2", "httpd"} {
		if _, err := exec.LookPath(bin); err == nil {
			s := &Server{
				Type:    TypeApache,
				Version: runOutput(bin, "-v"),
				ConfigPath: findFirst(
					"/etc/apache2/apache2.conf",
					"/etc/httpd/conf/httpd.conf",
					"/etc/httpd/httpd.conf",
				),
				Running: processRunning("apache2") || processRunning("httpd"),
			}
			if s.ConfigPath != "" {
				s.VHosts = parseApacheVHosts(s.ConfigPath)
			}
			return s
		}
	}
	return nil
}

func detectCaddy() *Server {
	if _, err := exec.LookPath("caddy"); err != nil {
		return nil
	}
	return &Server{
		Type:       TypeCaddy,
		Version:    runOutput("caddy", "version"),
		ConfigPath: findFirst("/etc/caddy/Caddyfile"),
		Running:    processRunning("caddy"),
		VHosts:     parseCaddyVHosts(),
	}
}

func parseNginxVHosts() []VHost {
	var vhosts []VHost

	// Check sites-available and sites-enabled
	enabledDir := "/etc/nginx/sites-enabled"
	availableDir := "/etc/nginx/sites-available"

	enabled := map[string]bool{}
	if entries, err := os.ReadDir(enabledDir); err == nil {
		for _, e := range entries {
			enabled[e.Name()] = true
		}
	}

	// Walk sites-available
	entries, err := os.ReadDir(availableDir)
	if err != nil {
		// No sites-available — try conf.d
		entries, _ = os.ReadDir("/etc/nginx/conf.d")
		availableDir = "/etc/nginx/conf.d"
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(availableDir, e.Name())
		vh := parseNginxServerBlock(path)
		vh.Enabled = enabled[e.Name()]
		vhosts = append(vhosts, vh)
	}
	return vhosts
}

func parseNginxServerBlock(path string) VHost {
	vh := VHost{ConfigFile: path}
	f, err := os.Open(path)
	if err != nil {
		return vh
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "server_name") {
			parts := strings.Fields(strings.TrimSuffix(line, ";"))
			if len(parts) >= 2 {
				vh.ServerName = parts[1]
				if len(parts) > 2 {
					vh.Aliases = parts[2:]
				}
			}
		}
		if strings.HasPrefix(line, "root") {
			parts := strings.Fields(strings.TrimSuffix(line, ";"))
			if len(parts) >= 2 {
				vh.Root = parts[1]
			}
		}
		if strings.HasPrefix(line, "listen") {
			if strings.Contains(line, "443") || strings.Contains(line, "ssl") {
				vh.SSL = true
				vh.Port = 443
			} else if vh.Port == 0 {
				vh.Port = 80
			}
		}
	}
	return vh
}

func parseApacheVHosts(configPath string) []VHost {
	var vhosts []VHost
	dir := filepath.Dir(configPath)

	// sites-available pattern
	dirs := []string{
		filepath.Join(dir, "sites-available"),
		filepath.Join(dir, "sites-enabled"),
		filepath.Join(dir, "conf.d"),
	}

	seen := map[string]bool{}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			path := filepath.Join(d, e.Name())
			vh := parseApacheVHost(path)
			vh.Enabled = strings.Contains(d, "enabled")
			vhosts = append(vhosts, vh)
		}
	}
	return vhosts
}

func parseApacheVHost(path string) VHost {
	vh := VHost{ConfigFile: path}
	f, err := os.Open(path)
	if err != nil {
		return vh
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "servername") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				vh.ServerName = parts[1]
			}
		}
		if strings.HasPrefix(lower, "serveralias") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				vh.Aliases = append(vh.Aliases, parts[1:]...)
			}
		}
		if strings.HasPrefix(lower, "documentroot") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				vh.Root = strings.Trim(parts[1], "\"")
			}
		}
		if strings.HasPrefix(lower, "<virtualhost") {
			if strings.Contains(line, ":443") || strings.Contains(line, "ssl") {
				vh.SSL = true
				vh.Port = 443
			} else {
				vh.Port = 80
			}
		}
	}
	return vh
}

func parseCaddyVHosts() []VHost {
	path := "/etc/caddy/Caddyfile"
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var vhosts []VHost
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Caddyfile site addresses are bare hostnames or host:port at line start
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "{") {
			continue
		}
		if strings.HasSuffix(line, "{") {
			host := strings.TrimSuffix(strings.TrimSpace(line), "{")
			host = strings.TrimSpace(host)
			if host != "" && !strings.Contains(host, " ") {
				vh := VHost{
					ServerName: host,
					ConfigFile: path,
					Enabled:    true,
					SSL:        !strings.HasPrefix(host, "http://"),
				}
				if vh.SSL {
					vh.Port = 443
				} else {
					vh.Port = 80
				}
				vhosts = append(vhosts, vh)
			}
		}
	}
	return vhosts
}

func testConfig(serverType Type, configPath string) error {
	switch serverType {
	case TypeNginx:
		out, err := exec.Command("nginx", "-tc", configPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
	case TypeApache:
		// Apache needs the real config path; just syntax check
		out, err := exec.Command("apachectl", "configtest").CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func findFirst(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func runOutput(cmd string, args ...string) string {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	if err != nil {
		return "unknown"
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) > 0 {
		return lines[0]
	}
	return "unknown"
}

func processRunning(name string) bool {
	out, err := exec.Command("pgrep", "-x", name).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}
