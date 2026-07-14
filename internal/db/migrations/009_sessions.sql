-- Server-side session store for JWT revocation on logout.
-- Replaces the pre-v0.9.8 schema (001_init.sql) which used user_id instead of username.
-- Sessions are short-lived (24h) so dropping existing ones on upgrade is safe.
DROP TABLE IF EXISTS webux_sessions;
CREATE TABLE webux_sessions (
    jti        TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    created_at DATETIME DEFAULT (datetime('now')),
    expires_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON webux_sessions(expires_at);
