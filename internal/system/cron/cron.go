// Package cron reads and manages crontab entries across all sources:
// /etc/crontab, /etc/cron.d/*, /var/spool/cron/crontabs/* (per-user).
package cron

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Source identifies where a cron job comes from.
type Source string

const (
	SourceEtcCrontab Source = "/etc/crontab"
	SourceCronD      Source = "cron.d"
	SourceUserSpool  Source = "user-spool"
)

// Job is a single crontab entry.
type Job struct {
	ID         string    `json:"id"`          // stable identifier: source+line
	Owner      string    `json:"owner"`       // user the job runs as
	Schedule   string    `json:"schedule"`    // e.g. "*/5 * * * *" or "@daily"
	Command    string    `json:"command"`
	Comment    string    `json:"comment"`     // inline comment if present
	SourceType Source    `json:"source_type"`
	SourceFile string    `json:"source_file"`
	LineNumber int       `json:"line_number"`
	NextRun    *time.Time `json:"next_run,omitempty"` // best-effort; nil if uncalculable
	Enabled    bool      `json:"enabled"`
}

// Manager handles all crontab sources.
type Manager struct{}

func NewManager() *Manager { return &Manager{} }

// List returns all cron jobs from all sources.
func (m *Manager) List() ([]Job, error) {
	var jobs []Job

	// /etc/crontab
	if j, err := parseFile("/etc/crontab", SourceEtcCrontab, true); err == nil {
		jobs = append(jobs, j...)
	}

	// /etc/cron.d/*
	if entries, err := os.ReadDir("/etc/cron.d"); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join("/etc/cron.d", e.Name())
			if j, err := parseFile(path, SourceCronD, true); err == nil {
				jobs = append(jobs, j...)
			}
		}
	}

	// Per-user spool /var/spool/cron/crontabs/
	for _, dir := range []string{"/var/spool/cron/crontabs", "/var/spool/cron"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			if j, err := parseFile(path, SourceUserSpool, false); err == nil {
				// Set owner to filename (username)
				for i := range j {
					j[i].Owner = e.Name()
				}
				jobs = append(jobs, j...)
			}
		}
	}

	return jobs, nil
}

// Add creates a new cron job. For system jobs, appends to /etc/cron.d/webux.
// For user jobs, uses `crontab` subprocess. Returns CLI equivalent.
func (m *Manager) Add(job Job) (string, error) {
	if job.Owner == "" || job.Owner == "root" || job.Owner == "system" {
		return m.addSystemJob(job)
	}
	return m.addUserJob(job)
}

func (m *Manager) addSystemJob(job Job) (string, error) {
	if err := validateSchedule(job.Schedule); err != nil {
		return "", err
	}
	path := "/etc/cron.d/webux"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return "", fmt.Errorf("open cron.d/webux: %w", err)
	}
	defer f.Close()

	owner := job.Owner
	if owner == "" {
		owner = "root"
	}
	line := fmt.Sprintf("%s %s %s\n", job.Schedule, owner, job.Command)
	if job.Comment != "" {
		line = "# " + job.Comment + "\n" + line
	}
	if _, err := f.WriteString(line); err != nil {
		return "", fmt.Errorf("write crontab: %w", err)
	}

	cmd := fmt.Sprintf("echo '%s %s %s' >> %s", job.Schedule, owner, job.Command, path)
	return cmd, nil
}

func (m *Manager) addUserJob(job Job) (string, error) {
	if err := validateSchedule(job.Schedule); err != nil {
		return "", err
	}
	// Read existing crontab
	existing, _ := exec.Command("crontab", "-l", "-u", job.Owner).Output()
	newLine := fmt.Sprintf("%s %s\n", job.Schedule, job.Command)
	updated := string(existing) + newLine

	// Write via stdin to crontab
	cmd := exec.Command("crontab", "-u", job.Owner, "-")
	cmd.Stdin = strings.NewReader(updated)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("crontab install: %w", err)
	}
	return fmt.Sprintf("(crontab -l -u %s; echo '%s %s') | crontab -u %s -",
		job.Owner, job.Schedule, job.Command, job.Owner), nil
}

// Delete removes a job by source file and line number. Returns CLI equivalent.
func (m *Manager) Delete(job Job) (string, error) {
	if job.SourceType == SourceUserSpool {
		return m.deleteUserJob(job)
	}
	return m.deleteFileJob(job)
}

func (m *Manager) deleteFileJob(job Job) (string, error) {
	data, err := os.ReadFile(job.SourceFile)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if job.LineNumber < 1 || job.LineNumber > len(lines) {
		return "", fmt.Errorf("invalid line number %d", job.LineNumber)
	}
	// Zero out the target line
	lines[job.LineNumber-1] = ""
	// Also remove the preceding comment line if it exists
	if job.LineNumber > 1 && strings.HasPrefix(strings.TrimSpace(lines[job.LineNumber-2]), "#") {
		lines[job.LineNumber-2] = ""
	}
	newContent := strings.Join(lines, "\n")
	if err := os.WriteFile(job.SourceFile, []byte(newContent), 0644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return fmt.Sprintf("# Line %d removed from %s", job.LineNumber, job.SourceFile), nil
}

func (m *Manager) deleteUserJob(job Job) (string, error) {
	out, err := exec.Command("crontab", "-l", "-u", job.Owner).Output()
	if err != nil {
		return "", fmt.Errorf("crontab -l: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	if job.LineNumber < 1 || job.LineNumber > len(lines) {
		return "", fmt.Errorf("invalid line number")
	}
	lines = append(lines[:job.LineNumber-1], lines[job.LineNumber:]...)
	cmd := exec.Command("crontab", "-u", job.Owner, "-")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("crontab install: %w", err)
	}
	return fmt.Sprintf("crontab -l -u %s | sed '%dd' | crontab -u %s -",
		job.Owner, job.LineNumber, job.Owner), nil
}

// Update replaces an existing cron job in-place. Returns CLI equivalent.
func (m *Manager) Update(old, updated Job) (string, error) {
	if err := validateSchedule(updated.Schedule); err != nil {
		return "", err
	}
	if old.SourceType == SourceUserSpool {
		return m.updateUserJob(old, updated)
	}
	return m.updateFileJob(old, updated)
}

func (m *Manager) updateFileJob(old, updated Job) (string, error) {
	data, err := os.ReadFile(old.SourceFile)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if old.LineNumber < 1 || old.LineNumber > len(lines) {
		return "", fmt.Errorf("invalid line number %d", old.LineNumber)
	}

	owner := updated.Owner
	if owner == "" {
		owner = "root"
	}

	// Rebuild the crontab line
	var newLine string
	// System files include a user field; user spools don't
	if old.SourceType == SourceEtcCrontab || old.SourceType == SourceCronD {
		newLine = fmt.Sprintf("%s %s %s", updated.Schedule, owner, updated.Command)
	} else {
		newLine = fmt.Sprintf("%s %s", updated.Schedule, updated.Command)
	}

	lines[old.LineNumber-1] = newLine
	if err := os.WriteFile(old.SourceFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return "", fmt.Errorf("write: %w", err)
	}
	return fmt.Sprintf("# Line %d in %s updated", old.LineNumber, old.SourceFile), nil
}

func (m *Manager) updateUserJob(old, updated Job) (string, error) {
	out, err := exec.Command("crontab", "-l", "-u", old.Owner).Output()
	if err != nil {
		return "", fmt.Errorf("crontab -l: %w", err)
	}
	lines := strings.Split(string(out), "\n")
	if old.LineNumber < 1 || old.LineNumber > len(lines) {
		return "", fmt.Errorf("invalid line number")
	}
	newLine := fmt.Sprintf("%s %s", updated.Schedule, updated.Command)
	lines[old.LineNumber-1] = newLine

	cmd := exec.Command("crontab", "-u", old.Owner, "-")
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n"))
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("crontab install: %w", err)
	}
	return fmt.Sprintf("# User crontab for %s updated at line %d", old.Owner, old.LineNumber), nil
}



// --- parsers ----------------------------------------------------------------

func parseFile(path string, sourceType Source, hasUserField bool) ([]Job, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var jobs []Job
	var pendingComment string
	lineNum := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			pendingComment = ""
			continue
		}

		// Capture comments to attach to next job
		if strings.HasPrefix(trimmed, "#") {
			pendingComment = strings.TrimPrefix(strings.TrimSpace(trimmed[1:]), " ")
			continue
		}

		// Skip variable assignments (NAME=value)
		if strings.Contains(trimmed, "=") && !strings.Contains(trimmed, " ") {
			pendingComment = ""
			continue
		}

		job := parseJobLine(trimmed, hasUserField)
		if job == nil {
			pendingComment = ""
			continue
		}
		job.Comment = pendingComment
		job.SourceType = sourceType
		job.SourceFile = path
		job.LineNumber = lineNum
		job.ID = fmt.Sprintf("%s:%d", path, lineNum)
		job.Enabled = true
		jobs = append(jobs, *job)
		pendingComment = ""
	}
	return jobs, scanner.Err()
}

func parseJobLine(line string, hasUserField bool) *Job {
	fields := strings.Fields(line)

	// Handle @special syntax (@daily, @weekly, etc.)
	if strings.HasPrefix(fields[0], "@") {
		if len(fields) < 2 {
			return nil
		}
		job := &Job{Schedule: fields[0]}
		if hasUserField && len(fields) >= 3 {
			job.Owner = fields[1]
			job.Command = strings.Join(fields[2:], " ")
		} else if len(fields) >= 2 {
			job.Command = strings.Join(fields[1:], " ")
		}
		return job
	}

	// Standard 5-field schedule: min hour dom mon dow [user] command
	minFields := 6
	if hasUserField {
		minFields = 7
	}
	if len(fields) < minFields {
		return nil
	}

	schedule := strings.Join(fields[:5], " ")
	job := &Job{Schedule: schedule}

	if hasUserField {
		job.Owner = fields[5]
		job.Command = strings.Join(fields[6:], " ")
	} else {
		job.Command = strings.Join(fields[5:], " ")
	}

	return job
}

// validateSchedule does a simple sanity check on a cron expression.
func validateSchedule(expr string) error {
	specials := map[string]bool{
		"@reboot": true, "@hourly": true, "@daily": true,
		"@weekly": true, "@monthly": true, "@yearly": true, "@annually": true,
	}
	if specials[expr] {
		return nil
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return fmt.Errorf("cron expression must have 5 fields (got %d)", len(fields))
	}
	return nil
}

// ValidateSchedule checks a cron expression for basic correctness.
func ValidateSchedule(expr string) error { return validateSchedule(expr) }
func CommonSchedules() []struct{ Label, Value string } {
	return []struct{ Label, Value string }{
		{"Every minute", "* * * * *"},
		{"Every 5 minutes", "*/5 * * * *"},
		{"Every 15 minutes", "*/15 * * * *"},
		{"Every 30 minutes", "*/30 * * * *"},
		{"Every hour", "0 * * * *"},
		{"Every 6 hours", "0 */6 * * *"},
		{"Daily at midnight", "0 0 * * *"},
		{"Daily at noon", "0 12 * * *"},
		{"Weekly (Sunday midnight)", "0 0 * * 0"},
		{"Monthly (1st at midnight)", "0 0 1 * *"},
		{"@reboot", "@reboot"},
		{"@daily", "@daily"},
		{"@weekly", "@weekly"},
		{"@monthly", "@monthly"},
	}
}
