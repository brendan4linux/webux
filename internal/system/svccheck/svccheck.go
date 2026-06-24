// Package svccheck provides security checks for individual services
// (nginx, apache, mysql/mariadb) using only OS-level inspection.
package svccheck

// Check represents a single security check result.
type Check struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Pass     bool   `json:"pass"`
	Detail   string `json:"detail"`   // what was found
	Fix      string `json:"fix"`      // how to remediate if failing
	Severity string `json:"severity"` // "high" | "medium" | "low"
}
