package initsys

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// OpenRC implements InitSystem for Alpine Linux and Gentoo systems.
// Uses rc-service and rc-update subprocesses — no dbus on OpenRC systems.
type OpenRC struct{}

// NewOpenRC returns an OpenRC manager.
func NewOpenRC() *OpenRC { return &OpenRC{} }

// OpenRCAvailable returns true if OpenRC is the active init system.
func OpenRCAvailable() bool {
	_, err := exec.LookPath("rc-service")
	if err != nil {
		return false
	}
	// Double-check: /run/openrc should exist on a running OpenRC system
	_, err = os.Stat("/run/openrc")
	return err == nil
}

func (o *OpenRC) Name() string { return "openrc" }

// List returns all services known to OpenRC.
func (o *OpenRC) List(ctx context.Context, unitType string) ([]Unit, error) {
	// rc-status --all --nocolor lists all services and their state
	out, err := exec.CommandContext(ctx, "rc-status", "--all", "--nocolor").Output()
	if err != nil {
		return nil, fmt.Errorf("rc-status: %w", err)
	}

	// Get enabled services per runlevel
	enabledMap := o.enabledServices(ctx)

	var units []Unit
	scanner := bufio.NewScanner(bytes.NewReader(out))
	var currentRunlevel string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Runlevel headers look like "Runlevel: default"
		if strings.HasPrefix(line, "Runlevel:") {
			currentRunlevel = strings.TrimPrefix(line, "Runlevel: ")
			continue
		}

		// Service lines: " * sshd           [ started ]"
		if !strings.HasPrefix(line, " * ") {
			continue
		}
		parts := strings.Fields(trimmed[2:]) // strip " * "
		if len(parts) < 2 {
			continue
		}
		name := parts[0]
		state := strings.Trim(strings.Join(parts[1:], " "), "[]")
		state = strings.TrimSpace(state)

		activeState := "inactive"
		subState := state
		if state == "started" {
			activeState = "active"
		} else if state == "stopped" {
			activeState = "inactive"
		} else if state == "crashed" {
			activeState = "failed"
		}

		enabled := "disabled"
		if enabledMap[name] {
			enabled = "enabled"
		}

		units = append(units, Unit{
			Name:        name,
			Description: name, // OpenRC doesn't expose descriptions easily
			LoadState:   "loaded",
			ActiveState: activeState,
			SubState:    subState,
			UnitType:    "service",
			Enabled:     enabled,
			Following:   currentRunlevel,
		})
	}
	return units, nil
}

func (o *OpenRC) enabledServices(ctx context.Context) map[string]bool {
	out, err := exec.CommandContext(ctx, "rc-update", "show").Output()
	if err != nil {
		return nil
	}
	m := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 {
			m[fields[0]] = true
		}
	}
	return m
}

func (o *OpenRC) Get(ctx context.Context, name string) (*Unit, error) {
	units, err := o.List(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		if u.Name == name {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("unit %s not found", name)
}

func (o *OpenRC) Start(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-service %s start", name)
	if err := exec.CommandContext(ctx, "rc-service", name, "start").Run(); err != nil {
		return "", fmt.Errorf("start %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Stop(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-service %s stop", name)
	if err := exec.CommandContext(ctx, "rc-service", name, "stop").Run(); err != nil {
		return "", fmt.Errorf("stop %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Restart(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-service %s restart", name)
	if err := exec.CommandContext(ctx, "rc-service", name, "restart").Run(); err != nil {
		return "", fmt.Errorf("restart %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Reload(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-service %s reload", name)
	if err := exec.CommandContext(ctx, "rc-service", name, "reload").Run(); err != nil {
		return "", fmt.Errorf("reload %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Enable(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-update add %s default", name)
	if err := exec.CommandContext(ctx, "rc-update", "add", name, "default").Run(); err != nil {
		return "", fmt.Errorf("enable %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Disable(ctx context.Context, name string) (string, error) {
	cmd := fmt.Sprintf("rc-update del %s", name)
	if err := exec.CommandContext(ctx, "rc-update", "del", name).Run(); err != nil {
		return "", fmt.Errorf("disable %s: %w", name, err)
	}
	return cmd, nil
}

func (o *OpenRC) Logs(ctx context.Context, name string, lines int) ([]LogEntry, error) {
	// OpenRC logs to /var/log/rc.log or syslog — try both
	logFile := fmt.Sprintf("/var/log/%s.log", name)
	if _, err := os.Stat(logFile); err == nil {
		return tailLogFile(logFile, lines)
	}
	// Fallback: grep syslog
	out, err := exec.CommandContext(ctx, "grep", name, "/var/log/messages").Output()
	if err != nil {
		return nil, nil // no logs available — not an error
	}
	var entries []LogEntry
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" {
			entries = append(entries, LogEntry{Message: line})
		}
	}
	if lines > 0 && len(entries) > lines {
		entries = entries[len(entries)-lines:]
	}
	return entries, nil
}

func tailLogFile(path string, n int) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var entries []LogEntry
	for _, l := range lines {
		entries = append(entries, LogEntry{Message: l})
	}
	return entries, nil
}
