package handlers

import (
	"net/http"
	"strings"

	"github.com/brendan4linux/webux/internal/system/svccheck"
)

// Security handles GET /api/webservers/security?type=nginx|apache
func (h *WebserversHandler) Security(w http.ResponseWriter, r *http.Request) {
	svcType := strings.ToLower(r.URL.Query().Get("type"))
	var checks []svccheck.Check
	switch svcType {
	case "nginx":
		checks = svccheck.RunNginx()
	case "apache", "apache2", "httpd":
		checks = svccheck.RunApache()
	default:
		// Run both and merge
		checks = append(svccheck.RunNginx(), svccheck.RunApache()...)
	}

	passed, failed := 0, 0
	for _, c := range checks {
		if c.Pass {
			passed++
		} else {
			failed++
		}
	}
	writeJSON(w, map[string]any{
		"type":   svcType,
		"checks": checks,
		"passed": passed,
		"failed": failed,
	})
}
