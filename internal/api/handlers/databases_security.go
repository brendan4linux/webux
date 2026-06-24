package handlers

import (
	"net/http"

	"github.com/brendan4linux/webux/internal/system/svccheck"
)

// Security handles GET /api/databases/security
func (h *DatabasesHandler) Security(w http.ResponseWriter, r *http.Request) {
	checks := svccheck.RunMySQL()
	passed, failed := 0, 0
	for _, c := range checks {
		if c.Pass {
			passed++
		} else {
			failed++
		}
	}
	writeJSON(w, map[string]any{
		"checks": checks,
		"passed": passed,
		"failed": failed,
	})
}
