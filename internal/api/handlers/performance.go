package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/brendan4linux/webux/internal/system/performance"
)

const perfCacheKey = "performance.results.v2"
const perfCacheTTL = 24 * time.Hour

type PerformanceHandler struct {
	db *sql.DB
}

func NewPerformanceHandler(db *sql.DB) *PerformanceHandler {
	return &PerformanceHandler{db: db}
}

// Get handles GET /api/performance
func (h *PerformanceHandler) Get(w http.ResponseWriter, r *http.Request) {
	if score := h.loadCached(); score != nil {
		writeJSON(w, score)
		return
	}
	score := performance.RunAll()
	h.saveCache(score)
	writeJSON(w, score)
}

// Refresh handles POST /api/performance/refresh
func (h *PerformanceHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	score := performance.RunAll()
	h.saveCache(score)
	writeJSON(w, score)
}

func (h *PerformanceHandler) loadCached() *performance.Score {
	var raw string
	err := h.db.QueryRow(
		`SELECT value FROM webux_settings WHERE key = ?`, perfCacheKey,
	).Scan(&raw)
	if err != nil || raw == "" {
		return nil
	}
	var score performance.Score
	if err := json.Unmarshal([]byte(raw), &score); err != nil {
		return nil
	}
	if time.Since(score.RunAt) > perfCacheTTL {
		return nil
	}
	return &score
}

func (h *PerformanceHandler) saveCache(score *performance.Score) {
	b, err := json.Marshal(score)
	if err != nil {
		return
	}
	h.db.Exec(
		`INSERT INTO webux_settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		perfCacheKey, string(b),
	)
}
