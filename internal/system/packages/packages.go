// Package packages provides a unified interface over pacman (Arch),
// apt (Debian/Ubuntu), and dnf/yum (RHEL/Fedora), plus Flatpak.
// Detection is automatic based on /etc/os-release.
package packages

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Family identifies the package manager family.
type Family string

const (
	FamilyPacman Family = "pacman" // Arch, Manjaro, EndeavourOS
	FamilyApt    Family = "apt"    // Debian, Ubuntu, Mint, Pop!_OS
	FamilyDNF    Family = "dnf"    // Fedora, RHEL 8+, CentOS Stream
	FamilyYum    Family = "yum"    // RHEL 7, older CentOS
	FamilyUnknown Family = "unknown"
)

// Package is an installed or available package.
type Package struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	NewVersion  string    `json:"new_version,omitempty"` // set if upgrade available
	Description string    `json:"description"`
	Size        string    `json:"size,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	Installed   bool      `json:"installed"`
	Upgradable  bool      `json:"upgradable"`
	InstallDate *time.Time `json:"install_date,omitempty"`
}

// FlatpakApp is an installed Flatpak application.
type FlatpakApp struct {
	Name        string `json:"name"`
	AppID       string `json:"app_id"`
	Version     string `json:"version"`
	Branch      string `json:"branch"`
	Origin      string `json:"origin"`
	InstallType string `json:"install_type"` // system | user
}

// Manager handles package operations for the detected distro.
type Manager struct {
	Family  Family
	Backend string // actual binary: pacman, apt, dnf, yum
}

// NewManager detects the package manager and returns a Manager.
func NewManager() *Manager {
	m := &Manager{Family: detectFamily()}
	switch m.Family {
	case FamilyPacman:
		m.Backend = "pacman"
	case FamilyApt:
		m.Backend = "apt"
	case FamilyDNF:
		m.Backend = "dnf"
	case FamilyYum:
		m.Backend = "yum"
	}
	return m
}

// ── Detection ────────────────────────────────────────────────────────────

func detectFamily() Family {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return familyFromBinary()
	}
	content := strings.ToLower(string(data))

	// Check ID and ID_LIKE fields
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "id=") || strings.HasPrefix(line, "id_like=") {
			val := strings.Trim(strings.SplitN(line, "=", 2)[1], `"' `)
			if strings.Contains(val, "arch") || strings.Contains(val, "manjaro") {
				return FamilyPacman
			}
			if strings.Contains(val, "debian") || strings.Contains(val, "ubuntu") {
				return FamilyApt
			}
			if strings.Contains(val, "fedora") || strings.Contains(val, "rhel") ||
				strings.Contains(val, "centos") || strings.Contains(val, "rocky") ||
				strings.Contains(val, "alma") {
				if _, err := exec.LookPath("dnf"); err == nil {
					return FamilyDNF
				}
				return FamilyYum
			}
		}
	}
	return familyFromBinary()
}

func familyFromBinary() Family {
	switch {
	case binaryExists("pacman"):
		return FamilyPacman
	case binaryExists("apt"):
		return FamilyApt
	case binaryExists("dnf"):
		return FamilyDNF
	case binaryExists("yum"):
		return FamilyYum
	default:
		return FamilyUnknown
	}
}

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ── List installed ────────────────────────────────────────────────────────

// ListInstalled returns all installed packages.
func (m *Manager) ListInstalled() ([]Package, error) {
	switch m.Family {
	case FamilyPacman:
		return m.pacmanListInstalled()
	case FamilyApt:
		return m.aptListInstalled()
	case FamilyDNF, FamilyYum:
		return m.rpmListInstalled()
	default:
		return nil, fmt.Errorf("no supported package manager detected")
	}
}

func (m *Manager) pacmanListInstalled() ([]Package, error) {
	// pacman -Qi gives verbose info per package; -Q gives terse list
	out, err := exec.Command("pacman", "-Q").Output()
	if err != nil {
		return nil, fmt.Errorf("pacman -Q: %w", err)
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 {
			pkgs = append(pkgs, Package{
				Name:      fields[0],
				Version:   fields[1],
				Installed: true,
			})
		}
	}
	return pkgs, nil
}

func (m *Manager) aptListInstalled() ([]Package, error) {
	// dpkg-query is faster and more reliable than apt list
	out, err := exec.Command("dpkg-query",
		"-W", "-f=${Package}\t${Version}\t${Installed-Size}\t${binary:Summary}\n").Output()
	if err != nil {
		return nil, fmt.Errorf("dpkg-query: %w", err)
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 4)
		if len(parts) < 2 {
			continue
		}
		p := Package{
			Name:      parts[0],
			Version:   parts[1],
			Installed: true,
		}
		if len(parts) >= 3 {
			p.Size = parts[2] + " KB"
		}
		if len(parts) >= 4 {
			p.Description = parts[3]
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

func (m *Manager) rpmListInstalled() ([]Package, error) {
	out, err := exec.Command("rpm", "-qa", "--queryformat",
		"%{NAME}\t%{VERSION}-%{RELEASE}\t%{SIZE}\t%{SUMMARY}\n").Output()
	if err != nil {
		return nil, fmt.Errorf("rpm -qa: %w", err)
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 4)
		if len(parts) < 2 {
			continue
		}
		p := Package{
			Name:      parts[0],
			Version:   parts[1],
			Installed: true,
		}
		if len(parts) >= 3 {
			p.Size = parts[2]
		}
		if len(parts) >= 4 {
			p.Description = parts[3]
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// ── Upgradable ────────────────────────────────────────────────────────────

// ListUpgradable returns packages that have a newer version available.
func (m *Manager) ListUpgradable() ([]Package, error) {
	switch m.Family {
	case FamilyPacman:
		return m.pacmanUpgradable()
	case FamilyApt:
		return m.aptUpgradable()
	case FamilyDNF, FamilyYum:
		return m.rpmUpgradable()
	default:
		return nil, fmt.Errorf("unsupported package manager")
	}
}

func (m *Manager) pacmanUpgradable() ([]Package, error) {
	out, err := exec.Command("pacman", "-Qu").Output()
	if err != nil {
		// exit 1 with no output = nothing to upgrade
		return nil, nil
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		// format: name oldver -> newver
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 4 {
			pkgs = append(pkgs, Package{
				Name:       fields[0],
				Version:    fields[1],
				NewVersion: fields[3],
				Upgradable: true,
				Installed:  true,
			})
		}
	}
	return pkgs, nil
}

func (m *Manager) aptUpgradable() ([]Package, error) {
	// Run apt-get -s upgrade to simulate — reads the apt cache, no network
	out, err := exec.Command("apt-get", "-s", "upgrade").Output()
	if err != nil {
		return nil, fmt.Errorf("apt-get -s upgrade: %w", err)
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		// Lines starting with "Inst " = will be installed/upgraded
		if strings.HasPrefix(line, "Inst ") {
			fields := strings.Fields(line)
			p := Package{Upgradable: true, Installed: true}
			if len(fields) >= 2 {
				p.Name = fields[1]
			}
			if len(fields) >= 3 {
				p.Version = strings.Trim(fields[2], "[]")
			}
			if len(fields) >= 4 {
				p.NewVersion = strings.Trim(fields[3], "()")
			}
			pkgs = append(pkgs, p)
		}
	}
	return pkgs, nil
}

func (m *Manager) rpmUpgradable() ([]Package, error) {
	bin := "dnf"
	if m.Family == FamilyYum {
		bin = "yum"
	}
	out, err := exec.Command(bin, "check-update", "--quiet").Output()
	// dnf/yum exits 100 when updates available, 0 when none
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 100 {
			return nil, fmt.Errorf("%s check-update: %w", bin, err)
		}
	}
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && !strings.HasPrefix(fields[0], "Last") {
			pkgs = append(pkgs, Package{
				Name:       strings.SplitN(fields[0], ".", 2)[0],
				NewVersion: fields[1],
				Repo:       fields[2],
				Upgradable: true,
				Installed:  true,
			})
		}
	}
	return pkgs, nil
}

// ── Install / Remove / Upgrade ────────────────────────────────────────────

// Install installs a package. Streams output to out channel.
// Returns CLI equivalent.
func (m *Manager) Install(ctx context.Context, name string, out chan<- string) (string, error) {
	var args []string
	var cliCmd string
	switch m.Family {
	case FamilyPacman:
		args = []string{"pacman", "-S", "--noconfirm", name}
		cliCmd = "pacman -S " + name
	case FamilyApt:
		args = []string{"apt-get", "install", "-y", name}
		cliCmd = "apt-get install -y " + name
	case FamilyDNF:
		args = []string{"dnf", "install", "-y", name}
		cliCmd = "dnf install -y " + name
	case FamilyYum:
		args = []string{"yum", "install", "-y", name}
		cliCmd = "yum install -y " + name
	default:
		return "", fmt.Errorf("no supported package manager")
	}
	return cliCmd, m.runStreaming(ctx, args, out)
}

// Remove removes a package. Returns CLI equivalent.
func (m *Manager) Remove(ctx context.Context, name string, out chan<- string) (string, error) {
	var args []string
	var cliCmd string
	switch m.Family {
	case FamilyPacman:
		args = []string{"pacman", "-R", "--noconfirm", name}
		cliCmd = "pacman -R " + name
	case FamilyApt:
		args = []string{"apt-get", "remove", "-y", name}
		cliCmd = "apt-get remove -y " + name
	case FamilyDNF:
		args = []string{"dnf", "remove", "-y", name}
		cliCmd = "dnf remove -y " + name
	case FamilyYum:
		args = []string{"yum", "remove", "-y", name}
		cliCmd = "yum remove -y " + name
	default:
		return "", fmt.Errorf("no supported package manager")
	}
	return cliCmd, m.runStreaming(ctx, args, out)
}

// Upgrade upgrades all packages or a specific one. Returns CLI equivalent.
func (m *Manager) Upgrade(ctx context.Context, name string, out chan<- string) (string, error) {
	var args []string
	var cliCmd string
	switch m.Family {
	case FamilyPacman:
		if name != "" {
			args = []string{"pacman", "-S", "--noconfirm", name}
			cliCmd = "pacman -S " + name
		} else {
			args = []string{"pacman", "-Syu", "--noconfirm"}
			cliCmd = "pacman -Syu"
		}
	case FamilyApt:
		if name != "" {
			args = []string{"apt-get", "install", "--only-upgrade", "-y", name}
			cliCmd = "apt-get install --only-upgrade -y " + name
		} else {
			args = []string{"apt-get", "upgrade", "-y"}
			cliCmd = "apt-get upgrade -y"
		}
	case FamilyDNF:
		if name != "" {
			args = []string{"dnf", "upgrade", "-y", name}
			cliCmd = "dnf upgrade -y " + name
		} else {
			args = []string{"dnf", "upgrade", "-y"}
			cliCmd = "dnf upgrade -y"
		}
	case FamilyYum:
		args = []string{"yum", "update", "-y", name}
		cliCmd = "yum update -y " + name
	default:
		return "", fmt.Errorf("no supported package manager")
	}
	return cliCmd, m.runStreaming(ctx, args, out)
}

// Search searches for packages by name. Returns CLI equivalent.
func (m *Manager) Search(query string) ([]Package, string, error) {
	var cmd *exec.Cmd
	var cliCmd string
	switch m.Family {
	case FamilyPacman:
		cmd = exec.Command("pacman", "-Ss", query)
		cliCmd = "pacman -Ss " + query
	case FamilyApt:
		cmd = exec.Command("apt-cache", "search", query)
		cliCmd = "apt-cache search " + query
	case FamilyDNF:
		cmd = exec.Command("dnf", "search", query)
		cliCmd = "dnf search " + query
	case FamilyYum:
		cmd = exec.Command("yum", "search", query)
		cliCmd = "yum search " + query
	default:
		return nil, "", fmt.Errorf("no supported package manager")
	}

	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, cliCmd, fmt.Errorf("search failed: %w", err)
	}

	pkgs := parseSearchOutput(m.Family, string(out))
	return pkgs, cliCmd, nil
}

func parseSearchOutput(family Family, out string) []Package {
	var pkgs []Package
	scanner := bufio.NewScanner(strings.NewReader(out))

	switch family {
	case FamilyPacman:
		// Format: repo/name version\n    description
		var current Package
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				current.Description = strings.TrimSpace(line)
				pkgs = append(pkgs, current)
				current = Package{}
			} else {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					parts := strings.SplitN(fields[0], "/", 2)
					if len(parts) == 2 {
						current.Repo = parts[0]
						current.Name = parts[1]
					} else {
						current.Name = fields[0]
					}
					current.Version = fields[1]
				}
			}
		}
	case FamilyApt:
		// Format: name - description
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, " - ", 2)
			p := Package{Name: strings.TrimSpace(parts[0])}
			if len(parts) == 2 {
				p.Description = parts[1]
			}
			pkgs = append(pkgs, p)
		}
	default:
		// DNF/YUM: varied format, just extract names
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "Last") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 1 {
				name := strings.SplitN(fields[0], ".", 2)[0]
				p := Package{Name: name}
				if len(fields) >= 2 {
					p.Description = strings.Join(fields[1:], " ")
				}
				pkgs = append(pkgs, p)
			}
		}
	}
	return pkgs
}

// UpdateCache refreshes the package cache (apt update, pacman -Sy etc).
// Returns CLI equivalent.
func (m *Manager) UpdateCache(ctx context.Context, out chan<- string) (string, error) {
	var args []string
	var cliCmd string
	switch m.Family {
	case FamilyPacman:
		args = []string{"pacman", "-Sy"}
		cliCmd = "pacman -Sy"
	case FamilyApt:
		args = []string{"apt-get", "update"}
		cliCmd = "apt-get update"
	case FamilyDNF:
		args = []string{"dnf", "makecache"}
		cliCmd = "dnf makecache"
	case FamilyYum:
		args = []string{"yum", "makecache"}
		cliCmd = "yum makecache"
	default:
		return "", fmt.Errorf("no supported package manager")
	}
	return cliCmd, m.runStreaming(ctx, args, out)
}

// ── Flatpak ───────────────────────────────────────────────────────────────

// HasFlatpak returns true if flatpak is installed.
func HasFlatpak() bool { return binaryExists("flatpak") }

// ListFlatpaks returns all installed Flatpak applications.
func ListFlatpaks() ([]FlatpakApp, error) {
	// Try --columns with tab separator; fall back to default output
	out, err := exec.Command("flatpak", "list",
		"--columns=name,application,version,branch,origin,installation").Output()
	if err != nil {
		// Some versions don't support --columns — use plain list
		out, err = exec.Command("flatpak", "list").Output()
		if err != nil {
			return nil, fmt.Errorf("flatpak list: %w", err)
		}
		return parseFlatpakPlain(string(out)), nil
	}
	apps := parseFlatpakColumns(string(out))
	if len(apps) == 0 {
		// Columns flag produced no output — fall back
		out2, err2 := exec.Command("flatpak", "list").Output()
		if err2 == nil {
			return parseFlatpakPlain(string(out2)), nil
		}
	}
	return apps, nil
}

func parseFlatpakColumns(out string) []FlatpakApp {
	var apps []FlatpakApp
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Try tab split first, then collapse-whitespace split
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			// Some versions pad with spaces — split on 2+ spaces
			parts = splitByMultiSpace(line)
		}
		if len(parts) < 2 {
			continue
		}
		app := FlatpakApp{
			Name:  strings.TrimSpace(parts[0]),
			AppID: strings.TrimSpace(parts[1]),
		}
		if len(parts) >= 3 { app.Version = strings.TrimSpace(parts[2]) }
		if len(parts) >= 4 { app.Branch  = strings.TrimSpace(parts[3]) }
		if len(parts) >= 5 { app.Origin  = strings.TrimSpace(parts[4]) }
		if len(parts) >= 6 { app.InstallType = strings.TrimSpace(parts[5]) }
		if app.AppID != "" {
			apps = append(apps, app)
		}
	}
	return apps
}

func parseFlatpakPlain(out string) []FlatpakApp {
	// Plain `flatpak list` output: Name\tAppID\tVersion\tBranch
	var apps []FlatpakApp
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			parts = splitByMultiSpace(line)
		}
		if len(parts) < 2 {
			continue
		}
		app := FlatpakApp{
			Name:  strings.TrimSpace(parts[0]),
			AppID: strings.TrimSpace(parts[1]),
		}
		if len(parts) >= 3 { app.Version = strings.TrimSpace(parts[2]) }
		if len(parts) >= 4 { app.Branch  = strings.TrimSpace(parts[3]) }
		apps = append(apps, app)
	}
	return apps
}

func splitByMultiSpace(s string) []string {
	// Split on 2+ consecutive spaces — handles column-aligned output
	var parts []string
	var cur strings.Builder
	spaceRun := 0
	for _, ch := range s {
		if ch == ' ' {
			spaceRun++
			if spaceRun >= 2 {
				if cur.Len() > 0 {
					parts = append(parts, cur.String())
					cur.Reset()
				}
				spaceRun = 0
			}
		} else {
			if spaceRun == 1 {
				cur.WriteByte(' ')
			}
			spaceRun = 0
			cur.WriteRune(ch)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// RemoveFlatpak removes a Flatpak app by AppID. Returns CLI equivalent.
func RemoveFlatpak(ctx context.Context, appID string, out chan<- string) (string, error) {
	cliCmd := "flatpak uninstall -y " + appID
	return cliCmd, runStreamingArgs(ctx, []string{"flatpak", "uninstall", "-y", appID}, out)
}

// UpdateFlatpaks updates all Flatpak apps. Returns CLI equivalent.
func UpdateFlatpaks(ctx context.Context, out chan<- string) (string, error) {
	return "flatpak update -y", runStreamingArgs(ctx, []string{"flatpak", "update", "-y"}, out)
}

// ── streaming helpers ─────────────────────────────────────────────────────

func (m *Manager) runStreaming(ctx context.Context, args []string, out chan<- string) error {
	return runStreamingArgs(ctx, args, out)
}

func runStreamingArgs(ctx context.Context, args []string, out chan<- string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command")
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	// Some package managers check if they're interactive; set DEBIAN_FRONTEND
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	combined := io.MultiReader(stdout, stderr)
	scanner := bufio.NewScanner(combined)
	for scanner.Scan() {
		select {
		case out <- scanner.Text():
		case <-ctx.Done():
			cmd.Process.Kill()
			return ctx.Err()
		}
	}
	return cmd.Wait()
}
