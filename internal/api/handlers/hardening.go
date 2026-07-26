package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/brendan4linux/webux/internal/system/hardening"
)

const hardeningCacheKey = "hardening.results.v3"
const hardeningCacheTTL = 24 * time.Hour

// HardeningHandler manages security hardening score checks.
type HardeningHandler struct {
	db *sql.DB
}

func NewHardeningHandler(db *sql.DB) *HardeningHandler {
	return &HardeningHandler{db: db}
}

// Get handles GET /api/hardening
// Returns cached results if < 24h old, otherwise re-runs checks.
func (h *HardeningHandler) Get(w http.ResponseWriter, r *http.Request) {
	if score := h.loadCached(); score != nil {
		writeJSON(w, score)
		return
	}
	score := hardening.RunAll()
	h.saveCache(score)
	writeJSON(w, score)
}

// Refresh handles POST /api/hardening/refresh
// Forces a re-run of all checks regardless of cache age.
func (h *HardeningHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	score := hardening.RunAll()
	h.saveCache(score)
	writeJSON(w, score)
}

func (h *HardeningHandler) loadCached() *hardening.Score {
	var raw string
	err := h.db.QueryRow(
		`SELECT value FROM webux_settings WHERE key = ?`, hardeningCacheKey,
	).Scan(&raw)
	if err != nil || raw == "" {
		return nil
	}
	var score hardening.Score
	if err := json.Unmarshal([]byte(raw), &score); err != nil {
		return nil
	}
	if time.Since(score.RunAt) > hardeningCacheTTL {
		return nil
	}
	return &score
}

func (h *HardeningHandler) saveCache(score *hardening.Score) {
	b, err := json.Marshal(score)
	if err != nil {
		return
	}
	h.db.Exec(
		`INSERT INTO webux_settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		hardeningCacheKey, string(b),
	)
}
