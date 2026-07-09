// Package auth provides authentication for Webux.
// Supports PAM (with -tags pam + CGO), /etc/shadow fallback (default),
// JWT session tokens, server-side revocation, and a configurable SSO bypass token.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey string

const claimsContextKey contextKey = "webux_claims"

// ClaimsFromContext returns the authenticated user's claims from the request context,
// or nil if the request is unauthenticated (e.g. SSO bypass or auth disabled).
func ClaimsFromContext(ctx context.Context) *Claims {
	v, _ := ctx.Value(claimsContextKey).(*Claims)
	return v
}

// ── JWT ──────────────────────────────────────────────────────────────────

const (
	jwtHeader     = `{"alg":"HS256","typ":"JWT"}`
	sessionCookie = "webux_session"
	sessionTTL    = 24 * time.Hour
)

// Claims is the JWT payload.
type Claims struct {
	Username  string `json:"sub"`
	JTI       string `json:"jti"` // session ID — used for server-side revocation
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// ManagerConfig configures the auth Manager.
type ManagerConfig struct {
	JWTSecret    []byte
	BypassToken  string
	DB           *sql.DB  // required for server-side session revocation
	AllowedUsers []string // if non-empty, only these usernames may authenticate
}

// Manager handles authentication and session management.
type Manager struct {
	jwtSecret    []byte
	bypassToken  string
	db           *sql.DB
	allowedUsers map[string]bool
}

// NewManager creates an auth manager.
func NewManager(cfg ManagerConfig) *Manager {
	if len(cfg.JWTSecret) == 0 {
		cfg.JWTSecret = make([]byte, 32)
		rand.Read(cfg.JWTSecret)
		log.Printf("WARN: webux auth: no jwt_secret configured — using ephemeral key; sessions will not survive restarts and will not be shared across instances")
	}
	allowed := make(map[string]bool, len(cfg.AllowedUsers))
	for _, u := range cfg.AllowedUsers {
		allowed[u] = true
	}
	return &Manager{
		jwtSecret:    cfg.JWTSecret,
		bypassToken:  cfg.BypassToken,
		db:           cfg.DB,
		allowedUsers: allowed,
	}
}

// Login authenticates the user, checks the allow-list, and returns a signed token.
func (m *Manager) Login(username, password string) (string, error) {
	if err := AuthenticatePAM(username, password); err != nil {
		return "", err
	}
	if len(m.allowedUsers) > 0 && !m.allowedUsers[username] {
		return "", fmt.Errorf("user %q is not permitted to access this panel", username)
	}
	return m.issueToken(username)
}

// issueToken creates a signed JWT and persists the session in the DB.
func (m *Manager) issueToken(username string) (string, error) {
	jti, err := generateID()
	if err != nil {
		return "", fmt.Errorf("session id: %w", err)
	}
	now := time.Now()
	exp := now.Add(sessionTTL)
	claims := Claims{
		Username:  username,
		JTI:       jti,
		IssuedAt:  now.Unix(),
		ExpiresAt: exp.Unix(),
	}
	header64 := base64.RawURLEncoding.EncodeToString([]byte(jwtHeader))
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	claims64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sig := m.sign(header64 + "." + claims64)
	token := header64 + "." + claims64 + "." + sig

	// Persist session for revocation support
	if m.db != nil {
		m.db.Exec(
			`INSERT INTO webux_sessions (jti, username, expires_at) VALUES (?, ?, datetime(?, 'unixepoch'))`,
			jti, username, exp.Unix(),
		)
		// Prune expired sessions opportunistically
		m.db.Exec(`DELETE FROM webux_sessions WHERE expires_at < datetime('now')`)
	}

	return token, nil
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.jwtSecret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify validates a JWT token string and returns the claims.
// Also checks the session is still active in the DB (not revoked by logout).
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
	// Server-side revocation check — if a session was logged out, its JTI is gone
	if m.db != nil && claims.JTI != "" {
		var count int
		m.db.QueryRow(
			`SELECT COUNT(*) FROM webux_sessions WHERE jti = ? AND expires_at > datetime('now')`,
			claims.JTI,
		).Scan(&count)
		if count == 0 {
			return nil, fmt.Errorf("session revoked or expired")
		}
	}
	return &claims, nil
}

// RevokeSession deletes the session from the DB, invalidating the token immediately.
func (m *Manager) RevokeSession(token string) {
	if m.db == nil {
		return
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return
	}
	var claims Claims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil || claims.JTI == "" {
		return
	}
	m.db.Exec(`DELETE FROM webux_sessions WHERE jti = ?`, claims.JTI)
}

// SetCookie writes the JWT as an HttpOnly, Secure cookie on the response.
func (m *Manager) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie from the browser.
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
	a := r.Header.Get("Authorization")
	if strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

// Middleware returns an HTTP middleware that enforces authentication and emits audit logs.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws") {
			next.ServeHTTP(w, r)
			return
		}

		// SSO bypass token — header only, constant-time comparison
		if m.bypassToken != "" {
			provided := r.Header.Get("X-Webux-Token")
			if subtle.ConstantTimeCompare([]byte(provided), []byte(m.bypassToken)) == 1 {
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
		claims, err := m.Verify(token)
		if err != nil {
			m.ClearCookie(w)
			redirectToLogin(w, r)
			return
		}

		// Allow-list check
		if len(m.allowedUsers) > 0 && !m.allowedUsers[claims.Username] {
			slog.Warn("audit: access denied — user not in allowed_users",
				"user", claims.Username, "ip", r.RemoteAddr, "path", path)
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}

		// Audit log — every authenticated API/WS request
		slog.Info("audit",
			"user", claims.Username,
			"method", r.Method,
			"path", path,
			"ip", r.RemoteAddr,
		)

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GenerateSecret generates a cryptographically random 32-byte hex secret.
func GenerateSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
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
	w.Header().Set("Content-Type", "application/json")
	http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
}
