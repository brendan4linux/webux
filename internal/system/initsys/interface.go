// Package initsys provides a distro-agnostic interface for managing
// init system units (services). Implementations exist for systemd (via
// dbus) and openrc (via subprocess fallback).
package initsys

import "context"

// Unit represents a single init system unit (service, socket, timer, etc).
type Unit struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	LoadState   string `json:"load_state"`   // loaded | not-found | masked
	ActiveState string `json:"active_state"` // active | inactive | failed | activating
	SubState    string `json:"sub_state"`    // running | dead | exited | etc.
	UnitType    string `json:"unit_type"`    // service | socket | timer | target | etc.
	Enabled     string `json:"enabled"`      // enabled | disabled | static | masked
	Following   string `json:"following"`    // if this unit follows another
}

// LogEntry is a single log line from a unit's journal.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Priority  int    `json:"priority"` // syslog priority 0-7
}

// InitSystem is the distro-agnostic interface for service management.
// Every mutating method returns the CLI equivalent command string so it
// can be emitted to the learn mode echo pane.
type InitSystem interface {
	// Name returns the implementation name e.g. "systemd", "openrc"
	Name() string

	// List returns all known units, optionally filtered by type.
	// Pass empty string for unitType to get all types.
	List(ctx context.Context, unitType string) ([]Unit, error)

	// Get returns a single unit by name (e.g. "nginx.service")
	Get(ctx context.Context, name string) (*Unit, error)

	// Start activates a unit. Returns the CLI equivalent.
	Start(ctx context.Context, name string) (cliCmd string, err error)

	// Stop deactivates a unit. Returns the CLI equivalent.
	Stop(ctx context.Context, name string) (cliCmd string, err error)

	// Restart restarts a unit. Returns the CLI equivalent.
	Restart(ctx context.Context, name string) (cliCmd string, err error)

	// Reload sends SIGHUP / reload signal to a unit. Returns the CLI equivalent.
	Reload(ctx context.Context, name string) (cliCmd string, err error)

	// Enable marks a unit to start at boot. Returns the CLI equivalent.
	Enable(ctx context.Context, name string) (cliCmd string, err error)

	// Disable prevents a unit from starting at boot. Returns the CLI equivalent.
	Disable(ctx context.Context, name string) (cliCmd string, err error)

	// Logs returns the last n log lines for a unit.
	Logs(ctx context.Context, name string, lines int) ([]LogEntry, error)
}

// Detect returns the appropriate InitSystem implementation for the host.
// Falls back gracefully: systemd → openrc → nil.
func Detect() InitSystem {
	if SystemdAvailable() {
		return NewSystemd()
	}
	if OpenRCAvailable() {
		return NewOpenRC()
	}
	return nil
}
