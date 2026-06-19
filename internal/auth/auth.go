// Package auth provides authentication for Webux.
// Supports PAM (with -tags pam + CGO), /etc/shadow fallback (default),
// JWT session tokens, and a configurable SSO bypass token.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

)

// ── JWT ──────────────────────────────────────────────────────────────────

const (
	jwtHeader    = `{"alg":"HS256","typ":"JWT"}`
	sessionCookie = "webux_session"
	sessionTTL   = 24 * time.Hour
)

// Claims is the JWT payload.
type Claims struct {
	Username string `json:"sub"`
	IssuedAt int64  `json:"iat"`
	ExpiresAt int64 `json:"exp"`
}

// Manager handles authentication and session management.
type Manager struct {
	jwtSecret   []byte
	bypassToken string // empty = disabled
}

// NewManager creates an auth manager.
// jwtSecret should be a random 32+ byte value loaded from config/DB.
// bypassToken is the SSO bypass token from config (empty = disabled).
func NewManager(jwtSecret []byte, bypassToken string) *Manager {
	if len(jwtSecret) == 0 {
		// Generate ephemeral secret — sessions won't survive restarts,
		// but better than panicking.
		jwtSecret = make([]byte, 32)
		rand.Read(jwtSecret)
	}
	return &Manager{
		jwtSecret:   jwtSecret,
		bypassToken: bypassToken,
	}
}

// Login authenticates the user and sets a JWT cookie.
// Returns the signed token on success.
func (m *Manager) Login(username, password string) (string, error) {
	if err := AuthenticatePAM(username, password); err != nil {
		return "", err
	}
	return m.issueToken(username)
}

// issueToken creates a signed JWT for the given username.
func (m *Manager) issueToken(username string) (string, error) {
	now := time.Now()
	claims := Claims{
		Username:  username,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(sessionTTL).Unix(),
	}
	header64 := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader))
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	claims64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := m.sign(header64 + "." + claims64)
	return header64 + "." + claims64 + "." + sig, nil
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.jwtSecret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify validates a JWT token string and returns the claims.
func (m *Manager) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid token format")
	}
	expected := m.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, fmt.Errorf("invalid token signature")
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, err
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, fmt.Errorf("token expired")
	}
	return &claims, nil
}

// SetCookie writes the JWT as an HttpOnly cookie on the response.
func (m *Manager) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie.
func (m *Manager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// TokenFromRequest extracts the JWT from cookie or Authorization header.
func (m *Manager) TokenFromRequest(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		return cookie.Value
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// Middleware returns an HTTP middleware that enforces authentication.
// Only /api/* and /ws* paths require a valid JWT — the SPA (HTML/JS/CSS)
// is always served so the frontend can show the login screen itself.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Always allow: auth endpoints, static assets, SPA shell
		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		// Only enforce auth on API and WebSocket paths
		// Everything else (root, /, /index.html) serves the SPA freely
		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws") {
			next.ServeHTTP(w, r)
			return
		}

		// SSO bypass token check
		if m.bypassToken != "" {
			if r.URL.Query().Get("token") == m.bypassToken ||
				r.Header.Get("X-Webux-Token") == m.bypassToken {
				token, err := m.issueToken("sso")
				if err == nil {
					m.SetCookie(w, token)
				}
				next.ServeHTTP(w, r)
				return
			}
		}

		// JWT validation
		token := m.TokenFromRequest(r)
		if token == "" {
			redirectToLogin(w, r)
			return
		}
		if _, err := m.Verify(token); err != nil {
			m.ClearCookie(w)
			redirectToLogin(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GenerateSecret generates a cryptographically random 32-byte hex secret.
func GenerateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// BuildInfo returns a string describing the active auth backend.
func BuildInfo() string { return buildInfo() }

// PAMAvailable reports whether PAM support is compiled in.
func PAMAvailable() bool { return pamAvailable() }

// ── Helpers ───────────────────────────────────────────────────────────────

func isPublicPath(path string) bool {
	public := []string{
		"/auth/login",
		"/auth/logout",
		"/auth/bypass",
		"/auth/whoami",
		"/assets/",
		"/favicon",
	}
	for _, p := range public {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	// API and WebSocket calls get 401 JSON — never redirect these
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws") {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	// Static assets and the SPA root get served normally — the SPA itself
	// handles showing the login screen when /auth/whoami returns 401.
	// Never redirect the root or asset paths — that causes infinite loops
	// because the # fragment is never sent to the server.
	http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
}
