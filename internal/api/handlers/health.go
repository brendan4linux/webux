package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/brendan4linux/webux/internal/system/health"
)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) getChecks() []health.Check {
	var s string
	h.db.QueryRow("SELECT value FROM webux_settings WHERE key='health.checks'").Scan(&s)
	return health.LoadChecks(s)
}

// Run handles GET /api/health — runs all enabled checks concurrently
func (h *HealthHandler) Run(w http.ResponseWriter, r *http.Request) {
	checks := h.getChecks()
	// Filter to enabled only
	var enabled []health.Check
	for _, c := range checks {
		if c.Enabled {
			enabled = append(enabled, c)
		}
	}
	results := health.Run(enabled)
	writeJSON(w, map[string]interface{}{
		"results": results,
		"total":   len(results),
		"passed":  countPassed(results),
		"failed":  countFailed(results),
	})
}

// Detail handles GET /api/health/detail?id=<id>
func (h *HealthHandler) Detail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	checks := h.getChecks()
	detail, err := health.RunDetail(checks, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]interface{}{"id": id, "detail": detail})
}

// GetChecks handles GET /api/health/checks — returns current check config
func (h *HealthHandler) GetChecks(w http.ResponseWriter, r *http.Request) {
	checks := h.getChecks()
	writeJSON(w, map[string]interface{}{"checks": checks})
}

// SaveChecks handles PUT /api/health/checks — saves custom check config
func (h *HealthHandler) SaveChecks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Checks []health.Check `json:"checks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	s := health.MarshalChecks(body.Checks)
	h.db.Exec(`INSERT INTO webux_settings (key,value) VALUES ('health.checks',?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`, s)
	writeJSON(w, map[string]interface{}{"ok": true})
}

// ResetChecks handles DELETE /api/health/checks — resets to defaults
func (h *HealthHandler) ResetChecks(w http.ResponseWriter, r *http.Request) {
	h.db.Exec("DELETE FROM webux_settings WHERE key='health.checks'")
	writeJSON(w, map[string]interface{}{"ok": true, "checks": health.DefaultChecks()})
}

func countPassed(results []health.Result) int {
	n := 0
	for _, r := range results {
		if r.Pass {
			n++
		}
	}
	return n
}

func countFailed(results []health.Result) int {
	n := 0
	for _, r := range results {
		if !r.Pass {
			n++
		}
	}
	return n
}
