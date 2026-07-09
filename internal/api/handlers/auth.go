package handlers

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/brendan4linux/webux/internal/auth"
)

// loginBucket tracks failed login attempts per IP.
type loginBucket struct {
	failures int
	resetAt  time.Time
}

var (
	loginMu      sync.Mutex
	loginBuckets = map[string]*loginBucket{}
)

const (
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
)

func loginRateLimited(r *http.Request) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	loginMu.Lock()
	defer loginMu.Unlock()
	b := loginBuckets[ip]
	now := time.Now()
	if b == nil || now.After(b.resetAt) {
		loginBuckets[ip] = &loginBucket{failures: 0, resetAt: now.Add(loginWindow)}
		b = loginBuckets[ip]
	}
	return b.failures >= loginMaxFailures
}

func loginRecordFailure(r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	loginMu.Lock()
	defer loginMu.Unlock()
	if b := loginBuckets[ip]; b != nil {
		b.failures++
	}
}

type AuthHandler struct {
	mgr *auth.Manager
}

func NewAuthHandler(mgr *auth.Manager) *AuthHandler {
	return &AuthHandler{mgr: mgr}
}

// Login handles POST /auth/login
// Body: {"username":"root","password":"..."}
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if loginRateLimited(r) {
		http.Error(w, `{"error":"too many login attempts, try again later"}`, http.StatusTooManyRequests)
		return
	}

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
		loginRecordFailure(r)
		// Generic error — don't reveal whether user exists
		http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
		return
	}

	h.mgr.SetCookie(w, token)
	writeJSON(w, map[string]interface{}{
		"ok":       true,
		"username": body.Username,
	})
}

// Logout handles POST /auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Revoke the session server-side before clearing the cookie
	if token := h.mgr.TokenFromRequest(r); token != "" {
		h.mgr.RevokeSession(token)
	}
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
