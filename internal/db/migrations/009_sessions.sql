-- Server-side session store for JWT revocation on logout.
CREATE TABLE IF NOT EXISTS webux_sessions (
    jti        TEXT PRIMARY KEY,
    username   TEXT NOT NULL,
    created_at DATETIME DEFAULT (datetime('now')),
    expires_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON webux_sessions(expires_at);
