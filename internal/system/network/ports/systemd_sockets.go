// Package ports - systemd socket enrichment.
// Reads systemd .socket unit files from standard locations to annotate
// PortInfo entries with their owning systemd socket unit name.
package ports

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// SystemdSocketUnit represents a parsed .socket unit file.
type SystemdSocketUnit struct {
	Name        string   // e.g. "ssh.socket"
	Description string
	ListenPorts []uint16 // ports declared in ListenStream= / ListenDatagram=
	ListenAddrs []string // raw values (may include paths for Unix sockets)
	ActiveState string   // "active" | "inactive" | unknown
}

// systemdSocketDirs are the standard locations for systemd unit files.
var systemdSocketDirs = []string{
	"/etc/systemd/system",
	"/run/systemd/system",
	"/usr/lib/systemd/system",
	"/lib/systemd/system",
}

// LoadSystemdSockets discovers all .socket units and returns them.
// If systemd is not present this returns an empty slice without error.
func LoadSystemdSockets() ([]SystemdSocketUnit, error) {
	seen := make(map[string]bool)
	var units []SystemdSocketUnit

	for _, dir := range systemdSocketDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // dir may not exist
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".socket") {
				continue
			}
			if seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true

			unit, err := parseSocketUnit(filepath.Join(dir, e.Name()), e.Name())
			if err != nil {
				continue
			}
			units = append(units, unit)
		}
	}
	return units, nil
}

// EnrichWithSystemdSockets annotates PortInfo entries that are owned by a
// systemd .socket unit. Matching is by port number.
func EnrichWithSystemdSockets(ports []PortInfo, units []SystemdSocketUnit) {
	// Build port → unit name index
	portToUnit := make(map[uint16]string)
	for _, u := range units {
		for _, p := range u.ListenPorts {
			portToUnit[p] = u.Name
		}
	}

	for i := range ports {
		if unitName, ok := portToUnit[ports[i].LocalPort]; ok {
			ports[i].SystemdSocket = unitName
		}
	}
}

// parseSocketUnit reads a .socket unit file and extracts listen directives.
func parseSocketUnit(path, name string) (SystemdSocketUnit, error) {
	f, err := os.Open(path)
	if err != nil {
		return SystemdSocketUnit{}, err
	}
	defer f.Close()

	unit := SystemdSocketUnit{Name: name}
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "Description":
			unit.Description = val
		case "ListenStream", "ListenDatagram", "ListenSequentialPacket":
			unit.ListenAddrs = append(unit.ListenAddrs, val)
			// val may be "80", "0.0.0.0:443", "[::]:443", "/run/foo.sock"
			if port, ok := extractPort(val); ok {
				unit.ListenPorts = append(unit.ListenPorts, port)
			}
		}
	}
	return unit, scanner.Err()
}

// extractPort pulls the port number from a ListenStream value like:
// "80", "0.0.0.0:80", "[::1]:80", "443"
// Returns false for Unix socket paths.
func extractPort(val string) (uint16, bool) {
	if strings.HasPrefix(val, "/") || strings.HasPrefix(val, "@") {
		return 0, false // Unix domain socket
	}

	// Try plain port number first
	if p, err := strconv.ParseUint(val, 10, 16); err == nil {
		return uint16(p), true
	}

	// "host:port" or "[ipv6]:port"
	if idx := strings.LastIndex(val, ":"); idx != -1 {
		portStr := val[idx+1:]
		if p, err := strconv.ParseUint(portStr, 10, 16); err == nil {
			return uint16(p), true
		}
	}
	return 0, false
}

// CLIEquivalentSockets returns the shell command to list systemd socket units.
func CLIEquivalentSockets() string {
	return fmt.Sprintf("systemctl list-sockets --all --no-pager")
}
