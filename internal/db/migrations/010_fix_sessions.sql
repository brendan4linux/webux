-- Migration 009 was a no-op on databases created before v0.9.8 because
-- webux_sessions already existed with a different schema (user_id instead
-- of username). This migration does the actual DROP + recreate.
-- Sessions are 24h-lived so dropping them on upgrade is safe.
DROP TABLE IF EXISTS webux_sessions;
CREATE TABLE webux_sessions (
    jti        TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    created_at DATETIME DEFAULT (datetime('now')),
    expires_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON webux_sessions(expires_at);
