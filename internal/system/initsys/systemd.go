package initsys

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	systemdDest      = "org.freedesktop.systemd1"
	systemdPath      = "/org/freedesktop/systemd1"
	systemdManagerIF = "org.freedesktop.systemd1.Manager"
	systemdUnitIF    = "org.freedesktop.systemd1.Unit"
	systemdServiceIF = "org.freedesktop.systemd1.Service"
	dbusPropsIF      = "org.freedesktop.DBus.Properties"
)

// Systemd implements InitSystem via the systemd1 dbus interface.
// This avoids forking a systemctl subprocess for every operation.
type Systemd struct {
	conn *dbus.Conn
}

// NewSystemd connects to the system dbus and returns a Systemd manager.
// Tries the system bus first (root), falls back to session bus.
func NewSystemd() *Systemd {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		// Non-root or system bus unavailable — try session bus
		conn, err = dbus.ConnectSessionBus()
		if err != nil {
			return &Systemd{}
		}
	}
	return &Systemd{conn: conn}
}

// SystemdAvailable returns true if systemd is the active init system.
func SystemdAvailable() bool {
	// Most reliable indicator: /run/systemd/private exists
	if _, err := os.Stat("/run/systemd/private"); err == nil {
		return true
	}
	// Fallback: check if PID 1 is systemd
	comm, err := os.ReadFile("/proc/1/comm")
	if err == nil && strings.TrimSpace(string(comm)) == "systemd" {
		return true
	}
	return false
}

func (s *Systemd) Name() string { return "systemd" }

// List returns all units visible to systemd, optionally filtered by type.
func (s *Systemd) List(ctx context.Context, unitType string) ([]Unit, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("dbus not connected")
	}

	obj := s.conn.Object(systemdDest, systemdPath)

	// ListUnits returns loaded (active + failed + some inactive) units
	var rawUnits [][]interface{}
	if err := obj.CallWithContext(ctx, systemdManagerIF+".ListUnits", 0).Store(&rawUnits); err != nil {
		return nil, fmt.Errorf("ListUnits: %w", err)
	}

	// ListUnitFiles returns ALL installed units including disabled ones
	enabledMap, _ := s.listUnitFiles(ctx)

	// Build from ListUnits first
	seen := map[string]bool{}
	var units []Unit
	for _, raw := range rawUnits {
		if len(raw) < 6 {
			continue
		}
		name, _ := raw[0].(string)
		desc, _ := raw[1].(string)
		load, _ := raw[2].(string)
		active, _ := raw[3].(string)
		sub, _ := raw[4].(string)
		following, _ := raw[5].(string)

		uType := unitTypeFrom(name)
		if unitType != "" && uType != unitType {
			continue
		}

		seen[name] = true
		units = append(units, Unit{
			Name:        name,
			Description: desc,
			LoadState:   load,
			ActiveState: active,
			SubState:    sub,
			UnitType:    uType,
			Following:   following,
			Enabled:     enabledMap[name],
		})
	}

	// Add units from ListUnitFiles that weren't in ListUnits
	// These are disabled/masked units that have never been started
	for name, enableState := range enabledMap {
		// Normalize: strip path prefix if present
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		}
		// Skip aliases, symlinks, and generated units — they duplicate real units
		switch enableState {
		case "alias", "indirect", "generated", "transient", "bad":
			continue
		}
		if seen[name] {
			continue
		}
		uType := unitTypeFrom(name)
		if unitType != "" && uType != unitType {
			continue
		}
		// Skip template units (name@.service) — they're not runnable instances
		if strings.Contains(name, "@.") {
			continue
		}
		seen[name] = true
		units = append(units, Unit{
			Name:        name,
			Description: "",
			LoadState:   "not-loaded",
			ActiveState: "inactive",
			SubState:    "dead",
			UnitType:    uType,
			Enabled:     enableState,
		})
	}

	return units, nil
}

// listUnitFiles returns a map of unit name → enabled state.
func (s *Systemd) listUnitFiles(ctx context.Context) (map[string]string, error) {
	obj := s.conn.Object(systemdDest, systemdPath)

	// ListUnitFiles returns: path, enablementState
	var rawFiles [][]interface{}
	if err := obj.CallWithContext(ctx, systemdManagerIF+".ListUnitFiles", 0).Store(&rawFiles); err != nil {
		return nil, err
	}

	m := make(map[string]string)
	for _, raw := range rawFiles {
		if len(raw) < 2 {
			continue
		}
		path, _ := raw[0].(string)
		state, _ := raw[1].(string)
		// Extract unit name from full path
		parts := strings.Split(path, "/")
		name := parts[len(parts)-1]
		m[name] = state
	}
	return m, nil
}

// Get returns a single unit's full state by name.
func (s *Systemd) Get(ctx context.Context, name string) (*Unit, error) {
	if s.conn == nil {
		return nil, fmt.Errorf("dbus not connected")
	}

	obj := s.conn.Object(systemdDest, systemdPath)

	var unitPath dbus.ObjectPath
	if err := obj.CallWithContext(ctx, systemdManagerIF+".GetUnit", 0, name).Store(&unitPath); err != nil {
		// Unit may not be loaded — try LoadUnit which will load it transiently
		if err2 := obj.CallWithContext(ctx, systemdManagerIF+".LoadUnit", 0, name).Store(&unitPath); err2 != nil {
			return nil, fmt.Errorf("GetUnit %s: %w", name, err)
		}
	}

	return s.getUnitProps(ctx, name, unitPath)
}

func (s *Systemd) getUnitProps(ctx context.Context, name string, path dbus.ObjectPath) (*Unit, error) {
	obj := s.conn.Object(systemdDest, path)

	getString := func(iface, prop string) string {
		v, err := obj.GetProperty(iface + "." + prop)
		if err != nil {
			return ""
		}
		str, _ := v.Value().(string)
		return str
	}

	enabledMap, _ := s.listUnitFiles(ctx)

	u := &Unit{
		Name:        name,
		Description: getString(systemdUnitIF, "Description"),
		LoadState:   getString(systemdUnitIF, "LoadState"),
		ActiveState: getString(systemdUnitIF, "ActiveState"),
		SubState:    getString(systemdUnitIF, "SubState"),
		Following:   getString(systemdUnitIF, "Following"),
		UnitType:    unitTypeFrom(name),
		Enabled:     enabledMap[name],
	}
	return u, nil
}

// Start activates a unit via dbus StartUnit.
func (s *Systemd) Start(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	var jobPath dbus.ObjectPath
	if err := obj.CallWithContext(ctx, systemdManagerIF+".StartUnit", 0, name, "replace").Store(&jobPath); err != nil {
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	return fmt.Sprintf("systemctl start %s", name), nil
}

// Stop deactivates a unit via dbus StopUnit.
func (s *Systemd) Stop(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	var jobPath dbus.ObjectPath
	if err := obj.CallWithContext(ctx, systemdManagerIF+".StopUnit", 0, name, "replace").Store(&jobPath); err != nil {
		return "", fmt.Errorf("stop %s: %w", name, err)
	}
	return fmt.Sprintf("systemctl stop %s", name), nil
}

// Restart restarts a unit via dbus RestartUnit.
func (s *Systemd) Restart(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	var jobPath dbus.ObjectPath
	if err := obj.CallWithContext(ctx, systemdManagerIF+".RestartUnit", 0, name, "replace").Store(&jobPath); err != nil {
		return "", fmt.Errorf("restart %s: %w", name, err)
	}
	return fmt.Sprintf("systemctl restart %s", name), nil
}

// Reload sends a reload signal to a unit via dbus ReloadUnit.
func (s *Systemd) Reload(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	var jobPath dbus.ObjectPath
	if err := obj.CallWithContext(ctx, systemdManagerIF+".ReloadUnit", 0, name, "replace").Store(&jobPath); err != nil {
		return "", fmt.Errorf("reload %s: %w", name, err)
	}
	return fmt.Sprintf("systemctl reload %s", name), nil
}

// Enable enables a unit at boot via dbus EnableUnitFiles.
func (s *Systemd) Enable(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	// EnableUnitFiles(files []string, runtime bool, force bool)
	if err := obj.CallWithContext(ctx, systemdManagerIF+".EnableUnitFiles", 0,
		[]string{name}, false, true).Err; err != nil {
		return "", fmt.Errorf("enable %s: %w", name, err)
	}
	// Reload daemon so the symlink is picked up
	_ = obj.CallWithContext(ctx, systemdManagerIF+".Reload", 0).Err
	return fmt.Sprintf("systemctl enable %s", name), nil
}

// Disable disables a unit at boot via dbus DisableUnitFiles.
func (s *Systemd) Disable(ctx context.Context, name string) (string, error) {
	if s.conn == nil {
		return "", fmt.Errorf("dbus not connected")
	}
	obj := s.conn.Object(systemdDest, systemdPath)
	if err := obj.CallWithContext(ctx, systemdManagerIF+".DisableUnitFiles", 0,
		[]string{name}, false).Err; err != nil {
		return "", fmt.Errorf("disable %s: %w", name, err)
	}
	_ = obj.CallWithContext(ctx, systemdManagerIF+".Reload", 0).Err
	return fmt.Sprintf("systemctl disable %s", name), nil
}

// Logs returns the last n lines from the journal for a unit.
// Uses journalctl subprocess — there is no stable dbus API for journal reads.
func (s *Systemd) Logs(ctx context.Context, name string, lines int) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 100
	}
	args := []string{
		"-u", name,
		"-n", fmt.Sprintf("%d", lines),
		"--no-pager",
		"--output=short-iso",
	}
	out, err := exec.CommandContext(ctx, "journalctl", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("journalctl: %w", err)
	}

	var entries []LogEntry
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		// short-iso format: "2024-01-15T12:34:56+0000 hostname unit[pid]: message"
		parts := strings.SplitN(line, " ", 3)
		entry := LogEntry{Message: line}
		if len(parts) >= 3 {
			entry.Timestamp = parts[0]
			entry.Message = parts[2]
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// unitTypeFrom extracts the type suffix from a unit name.
func unitTypeFrom(name string) string {
	if idx := strings.LastIndex(name, "."); idx != -1 {
		return name[idx+1:]
	}
	return "unknown"
}
