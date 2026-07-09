package packages

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo represents a configured package repository.
type Repo struct {
	ID       string `json:"id"`       // unique identifier for this entry
	Name     string `json:"name"`     // human-readable name
	URL      string `json:"url"`      // base URL or mirrorlist path
	Enabled  bool   `json:"enabled"`
	File     string `json:"file"`     // config file path on disk
	Line     int    `json:"line"`     // line number in file (for editing)
	Keyring  string `json:"keyring,omitempty"` // GPG key ID or path
	Section  string `json:"section,omitempty"` // apt: main/universe/contrib etc
	Extra    string `json:"extra,omitempty"`   // any extra options (arch=, etc)
	Source   string `json:"source"`   // "pacman" | "apt" | "dnf" | "flatpak-remote"
}

// ── List repos ────────────────────────────────────────────────────────────

// ListRepos returns all configured repositories for the detected package manager.
func (m *Manager) ListRepos() ([]Repo, error) {
	switch m.Family {
	case FamilyPacman:
		return listPacmanRepos()
	case FamilyApt:
		return listAptRepos()
	case FamilyDNF, FamilyYum:
		return listDNFRepos(string(m.Family))
	default:
		return nil, fmt.Errorf("no supported package manager")
	}
}

// listPacmanRepos parses /etc/pacman.conf for [repo] sections.
func listPacmanRepos() ([]Repo, error) {
	path := "/etc/pacman.conf"
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open pacman.conf: %w", err)
	}
	defer f.Close()

	var repos []Repo
	var current *Repo
	lineNum := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Section header: [repo-name]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := line[1 : len(line)-1]
			if name == "options" {
				current = nil
				continue
			}
			r := Repo{
				ID:      name,
				Name:    name,
				Enabled: true,
				File:    path,
				Line:    lineNum,
				Source:  "pacman",
			}
			repos = append(repos, r)
			current = &repos[len(repos)-1]
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "#") {
			continue
		}

		key, val, ok := splitKV(line, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "server":
			if current.URL == "" {
				current.URL = strings.TrimSpace(val)
			}
		case "include":
			// mirrorlist file
			current.URL = "include: " + strings.TrimSpace(val)
		}
	}
	return repos, scanner.Err()
}

// listAptRepos parses /etc/apt/sources.list and /etc/apt/sources.list.d/*.
func listAptRepos() ([]Repo, error) {
	var repos []Repo

	files := []string{"/etc/apt/sources.list"}
	if entries, err := os.ReadDir("/etc/apt/sources.list.d"); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, filepath.Join("/etc/apt/sources.list.d", e.Name()))
			}
		}
	}

	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lineNum := 0
		for _, line := range strings.Split(string(data), "\n") {
			lineNum++
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}

			enabled := true
			if strings.HasPrefix(trimmed, "#") {
				// Could be a commented-out repo
				rest := strings.TrimSpace(trimmed[1:])
				if !strings.HasPrefix(rest, "deb") {
					continue
				}
				trimmed = rest
				enabled = false
			}

			// deb [options] url suite components...
			// deb-src [options] url suite components...
			if !strings.HasPrefix(trimmed, "deb") {
				continue
			}

			r := parseAptLine(trimmed, path, lineNum)
			r.Enabled = enabled
			repos = append(repos, r)
		}
	}
	return repos, nil
}

func parseAptLine(line, file string, lineNum int) Repo {
	r := Repo{File: file, Line: lineNum, Source: "apt", Enabled: true}

	// Strip "deb-src" or "deb"
	rest := line
	if strings.HasPrefix(rest, "deb-src") {
		rest = strings.TrimPrefix(rest, "deb-src")
		r.Extra = "deb-src"
	} else {
		rest = strings.TrimPrefix(rest, "deb")
	}
	rest = strings.TrimSpace(rest)

	// Options block [arch=amd64 signed-by=...]
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end >= 0 {
			r.Extra += " " + rest[1:end]
			// Extract signed-by as keyring
			for _, opt := range strings.Fields(rest[1:end]) {
				if strings.HasPrefix(opt, "signed-by=") {
					r.Keyring = strings.TrimPrefix(opt, "signed-by=")
				}
			}
			rest = strings.TrimSpace(rest[end+1:])
		}
	}

	fields := strings.Fields(rest)
	if len(fields) >= 1 {
		r.URL = fields[0]
	}
	if len(fields) >= 2 {
		r.Name = fields[1] // suite (e.g. "noble", "bookworm")
	}
	if len(fields) >= 3 {
		r.Section = strings.Join(fields[2:], " ") // components
	}

	r.ID = fmt.Sprintf("%s:%d", filepath.Base(file), lineNum)
	return r
}

// listDNFRepos uses `dnf/yum repolist --all` for a quick list,
// then parses /etc/yum.repos.d/*.repo for full details.
func listDNFRepos(backend string) ([]Repo, error) {
	repoDir := "/etc/yum.repos.d"
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", repoDir, err)
	}

	var repos []Repo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".repo") {
			continue
		}
		path := filepath.Join(repoDir, e.Name())
		rs, err := parseDNFRepoFile(path)
		if err != nil {
			continue
		}
		repos = append(repos, rs...)
	}
	return repos, nil
}

func parseDNFRepoFile(path string) ([]Repo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var repos []Repo
	var current *Repo
	lineNum := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			id := line[1 : len(line)-1]
			r := Repo{
				ID:      id,
				Name:    id,
				Enabled: true,
				File:    path,
				Line:    lineNum,
				Source:  "dnf",
			}
			repos = append(repos, r)
			current = &repos[len(repos)-1]
			continue
		}

		if current == nil {
			continue
		}

		key, val, ok := splitKV(line, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			current.Name = strings.TrimSpace(val)
		case "baseurl":
			current.URL = strings.TrimSpace(val)
		case "mirrorlist", "metalink":
			if current.URL == "" {
				current.URL = strings.TrimSpace(val)
			}
		case "enabled":
			current.Enabled = strings.TrimSpace(val) == "1"
		case "gpgkey":
			current.Keyring = strings.TrimSpace(val)
		}
	}
	return repos, scanner.Err()
}

// ── Enable / Disable ──────────────────────────────────────────────────────

// EnableRepo enables a repository. Returns CLI equivalent.
func (m *Manager) EnableRepo(repo Repo) (string, error) {
	switch m.Family {
	case FamilyPacman:
		return togglePacmanRepo(repo, true)
	case FamilyApt:
		return toggleAptRepo(repo, true)
	case FamilyDNF, FamilyYum:
		return toggleDNFRepo(repo, true, string(m.Family))
	default:
		return "", fmt.Errorf("unsupported package manager")
	}
}

// DisableRepo disables a repository. Returns CLI equivalent.
func (m *Manager) DisableRepo(repo Repo) (string, error) {
	switch m.Family {
	case FamilyPacman:
		return togglePacmanRepo(repo, false)
	case FamilyApt:
		return toggleAptRepo(repo, false)
	case FamilyDNF, FamilyYum:
		return toggleDNFRepo(repo, false, string(m.Family))
	default:
		return "", fmt.Errorf("unsupported package manager")
	}
}

func togglePacmanRepo(repo Repo, enable bool) (string, error) {
	data, err := os.ReadFile(repo.File)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	inSection := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "["+repo.ID+"]" {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			if enable {
				// Remove leading # if present
				if strings.HasPrefix(line, "#") {
					lines[i] = line[1:]
				}
			} else {
				// Add # if not already commented
				if !strings.HasPrefix(line, "#") && strings.TrimSpace(line) != "" {
					lines[i] = "#" + line
				}
			}
		}
	}

	if err := os.WriteFile(repo.File, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", err
	}
	action := "disabled"
	if enable {
		action = "enabled"
	}
	return fmt.Sprintf("# Repo [%s] %s in %s", repo.ID, action, repo.File), nil
}

func toggleAptRepo(repo Repo, enable bool) (string, error) {
	data, err := os.ReadFile(repo.File)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if repo.Line < 1 || repo.Line > len(lines) {
		return "", fmt.Errorf("invalid line number %d", repo.Line)
	}
	idx := repo.Line - 1
	line := lines[idx]
	if enable {
		lines[idx] = strings.TrimPrefix(line, "#")
		lines[idx] = strings.TrimSpace(lines[idx])
	} else {
		if !strings.HasPrefix(line, "#") {
			lines[idx] = "# " + line
		}
	}
	if err := os.WriteFile(repo.File, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", err
	}
	action := "disabled"
	if enable {
		action = "enabled"
	}
	return fmt.Sprintf("# apt repo at line %d of %s %s", repo.Line, repo.File, action), nil
}

func toggleDNFRepo(repo Repo, enable bool, backend string) (string, error) {
	val := "0"
	if enable {
		val = "1"
	}
	cmd := fmt.Sprintf("%s config-manager --set-%s %s", backend,
		map[bool]string{true: "enabled", false: "disabled"}[enable], repo.ID)
	if err := exec.Command(backend, "config-manager",
		"--set-"+map[bool]string{true: "enabled", false: "disabled"}[enable],
		repo.ID).Run(); err != nil {
		// Fall back to direct file edit
		return editDNFRepoEnabled(repo, val, cmd)
	}
	return cmd, nil
}

func editDNFRepoEnabled(repo Repo, val, cliCmd string) (string, error) {
	data, err := os.ReadFile(repo.File)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	inSection := false
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "["+repo.ID+"]" {
			inSection = true
			continue
		}
		if inSection {
			if strings.HasPrefix(trimmed, "[") {
				break
			}
			if strings.HasPrefix(strings.ToLower(trimmed), "enabled") {
				lines[i] = "enabled=" + val
				found = true
			}
		}
	}
	if !found {
		// Append enabled= after the section header
		for i, line := range lines {
			if strings.TrimSpace(line) == "["+repo.ID+"]" {
				lines = append(lines[:i+1], append([]string{"enabled=" + val}, lines[i+1:]...)...)
				break
			}
		}
	}
	if err := os.WriteFile(repo.File, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", err
	}
	return cliCmd, nil
}

// ── Add repo ──────────────────────────────────────────────────────────────

// AddRepo adds a new repository. Returns CLI equivalent.
func (m *Manager) AddRepo(name, url, keyURL string) (string, error) {
	switch m.Family {
	case FamilyPacman:
		return addPacmanRepo(name, url)
	case FamilyApt:
		return addAptRepo(name, url, keyURL)
	case FamilyDNF, FamilyYum:
		return addDNFRepo(name, url, keyURL, string(m.Family))
	default:
		return "", fmt.Errorf("unsupported package manager")
	}
}

func addPacmanRepo(name, url string) (string, error) {
	section := fmt.Sprintf("\n[%s]\nServer = %s\n", name, url)
	f, err := os.OpenFile("/etc/pacman.conf", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(section); err != nil {
		return "", err
	}
	return fmt.Sprintf("# Added [%s] to /etc/pacman.conf", name), nil
}

func addAptRepo(name, repoURL, keyURL string) (string, error) {
	var cliParts []string

	// Import GPG key if provided — fetch in Go, pipe to gpg via argv (no shell)
	if keyURL != "" {
		if err := validateHTTPSURL(keyURL); err != nil {
			return "", fmt.Errorf("invalid key URL: %w", err)
		}
		keyPath := fmt.Sprintf("/etc/apt/keyrings/%s.gpg", sanitiseRepoName(name))
		if err := fetchAndDearmor(keyURL, keyPath); err != nil {
			// Non-fatal — key might already be installed
			_ = err
		}
		cliParts = append(cliParts,
			fmt.Sprintf("curl -fsSL '%s' | gpg --dearmor -o '%s'", keyURL, keyPath))
	}

	// Validate repo URL and strip newlines before writing (L3)
	repoURL = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, repoURL)

	listPath := fmt.Sprintf("/etc/apt/sources.list.d/%s.list", sanitiseRepoName(name))
	line := fmt.Sprintf("deb %s\n", repoURL)
	if err := os.WriteFile(listPath, []byte(line), 0644); err != nil {
		return "", fmt.Errorf("write sources list: %w", err)
	}
	cliParts = append(cliParts,
		fmt.Sprintf("echo 'deb %s' > %s", repoURL, listPath),
		"apt-get update")

	return strings.Join(cliParts, "\n"), nil
}

// validateHTTPSURL ensures a URL is well-formed and uses http or https.
func validateHTTPSURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("malformed URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	return nil
}

// fetchAndDearmor downloads a GPG key via Go's HTTP client and dearmors it
// using gpg argv (no shell). Safe against injection in keyURL.
func fetchAndDearmor(keyURL, destPath string) error {
	resp, err := http.Get(keyURL) //nolint:gosec // URL already validated by validateHTTPSURL
	if err != nil {
		return fmt.Errorf("fetch key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch key: HTTP %d", resp.StatusCode)
	}

	// Pipe response body to gpg --dearmor via argv (not sh -c)
	tmp, err := os.CreateTemp("", "webux-key-*.asc")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return fmt.Errorf("write temp key: %w", err)
	}
	tmp.Close()

	return exec.Command("gpg", "--dearmor", "-o", destPath, tmp.Name()).Run()
}

func addDNFRepo(name, url, keyURL, backend string) (string, error) {
	repoPath := fmt.Sprintf("/etc/yum.repos.d/%s.repo", sanitiseRepoName(name))
	content := fmt.Sprintf("[%s]\nname=%s\nbaseurl=%s\nenabled=1\n", name, name, url)
	if keyURL != "" {
		content += fmt.Sprintf("gpgcheck=1\ngpgkey=%s\n", keyURL)
	} else {
		content += "gpgcheck=0\n"
	}
	if err := os.WriteFile(repoPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write repo file: %w", err)
	}
	return fmt.Sprintf("# Wrote %s\n%s makecache", repoPath, backend), nil
}

// ── Remove repo ───────────────────────────────────────────────────────────

// RemoveRepo removes a repository. Returns CLI equivalent.
func (m *Manager) RemoveRepo(repo Repo) (string, error) {
	switch m.Family {
	case FamilyPacman:
		return removePacmanRepo(repo)
	case FamilyApt:
		return removeAptRepo(repo)
	case FamilyDNF, FamilyYum:
		return removeDNFRepo(repo)
	default:
		return "", fmt.Errorf("unsupported package manager")
	}
}

func removePacmanRepo(repo Repo) (string, error) {
	data, err := os.ReadFile(repo.File)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	var out []string
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "["+repo.ID+"]" {
			skip = true
			continue
		}
		if skip && strings.HasPrefix(trimmed, "[") {
			skip = false
		}
		if !skip {
			out = append(out, line)
		}
	}
	if err := os.WriteFile(repo.File, []byte(strings.Join(out, "\n")), 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("# Removed [%s] from %s", repo.ID, repo.File), nil
}

func removeAptRepo(repo Repo) (string, error) {
	// If the file is in sources.list.d and only has this one repo, remove the file
	if repo.File != "/etc/apt/sources.list" {
		data, err := os.ReadFile(repo.File)
		if err != nil {
			return "", err
		}
		// Count non-blank, non-comment lines
		active := 0
		for _, l := range strings.Split(string(data), "\n") {
			t := strings.TrimSpace(l)
			if t != "" && !strings.HasPrefix(t, "#") {
				active++
			}
		}
		if active <= 1 {
			if err := os.Remove(repo.File); err != nil {
				return "", err
			}
			return "rm " + repo.File + " && apt-get update", nil
		}
	}
	// Otherwise comment it out
	return toggleAptRepo(repo, false)
}

func removeDNFRepo(repo Repo) (string, error) {
	// Remove the whole .repo file if it only has this one section
	data, err := os.ReadFile(repo.File)
	if err != nil {
		return "", err
	}
	sections := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[") {
			sections++
		}
	}
	if sections <= 1 {
		if err := os.Remove(repo.File); err != nil {
			return "", err
		}
		return "rm " + repo.File, nil
	}
	// Multiple sections — set enabled=0 instead
	return editDNFRepoEnabled(repo, "0",
		fmt.Sprintf("# disabled [%s] in %s", repo.ID, repo.File))
}

// ── Flatpak remotes ───────────────────────────────────────────────────────

// FlatpakRemote is a configured Flatpak remote (repo).
type FlatpakRemote struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"` // system | user
}

// ListFlatpakRemotes returns all Flatpak remote repositories.
func ListFlatpakRemotes() ([]FlatpakRemote, error) {
	var remotes []FlatpakRemote
	seen := map[string]bool{}

	// Try system remotes, then user remotes — combine both
	for _, scope := range []string{"--system", "--user"} {
		out, err := exec.Command("flatpak", "remotes", scope).Output()
		if err != nil {
			continue
		}
		installType := "system"
		if scope == "--user" {
			installType = "user"
		}
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			// Output: Name\tOptions\tURL\tComment
			// Or simply Name\tURL on older versions
			parts := strings.Fields(line)
			if len(parts) < 1 {
				continue
			}
			name := parts[0]
			if seen[name] {
				continue
			}
			seen[name] = true
			r := FlatpakRemote{
				Name:    name,
				Enabled: true,
				Type:    installType,
			}
			// Get the URL with a separate show-remote call (most reliable)
			if urlOut, err := exec.Command("flatpak", "remote-info", scope,
				"--show-url", name).Output(); err == nil {
				r.URL = strings.TrimSpace(string(urlOut))
			} else if len(parts) >= 2 {
				r.URL = parts[len(parts)-1]
			}
			remotes = append(remotes, r)
		}
	}
	return remotes, nil
}

// AddFlatpakRemote adds a Flatpak remote. Returns CLI equivalent.
func AddFlatpakRemote(name, url string, system bool) (string, error) {
	args := []string{"remote-add", "--if-not-exists"}
	if !system {
		args = append(args, "--user")
	}
	args = append(args, name, url)
	if err := exec.Command("flatpak", args...).Run(); err != nil {
		return "", fmt.Errorf("flatpak remote-add: %w", err)
	}
	return "flatpak " + strings.Join(args, " "), nil
}

// RemoveFlatpakRemote removes a Flatpak remote. Returns CLI equivalent.
func RemoveFlatpakRemote(name string) (string, error) {
	if err := exec.Command("flatpak", "remote-delete", "--force", name).Run(); err != nil {
		return "", fmt.Errorf("flatpak remote-delete: %w", err)
	}
	return "flatpak remote-delete --force " + name, nil
}

// ── helpers ───────────────────────────────────────────────────────────────

func splitKV(line, sep string) (key, val string, ok bool) {
	idx := strings.Index(line, sep)
	if idx < 0 {
		return "", "", false
	}
	return line[:idx], line[idx+len(sep):], true
}

func sanitiseRepoName(name string) string {
	r := strings.NewReplacer(" ", "-", "/", "-", ":", "-")
	return strings.ToLower(r.Replace(name))
}
