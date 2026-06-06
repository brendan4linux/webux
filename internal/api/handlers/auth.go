package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/brendan4linux/webux/internal/auth"
)

type AuthHandler struct {
	mgr *auth.Manager
}

func NewAuthHandler(mgr *auth.Manager) *AuthHandler {
	return &AuthHandler{mgr: mgr}
}

// Login handles POST /auth/login
// Body: {"username":"root","password":"..."}
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
		http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
		return
	}

	token, err := h.mgr.Login(body.Username, body.Password)
	if err != nil {
		// Generic error — don't reveal whether user exists
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	h.mgr.SetCookie(w, token)
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"username": body.Username,
		"token":    token,
	})
}

// Logout handles POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	h.mgr.ClearCookie(w)
	writeJSON(w, map[string]interface{}{"ok": true})
}

// WhoAmI handles GET /auth/whoami — returns current user info
func (h *AuthHandler) WhoAmI(w http.ResponseWriter, r *http.Request) {
	token := h.mgr.TokenFromRequest(r)
	if token == "" {
		http.Error(w, `{"error":"not authenticated"}`, http.StatusUnauthorized)
		return
	}
	claims, err := h.mgr.Verify(token)
	if err != nil {
		http.Error(w, `{"error":"invalid session"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]interface{}{
		"username":      claims.Username,
		"pam_available": auth.PAMAvailable(),
		"auth_backend":  auth.BuildInfo(),
	})
}
