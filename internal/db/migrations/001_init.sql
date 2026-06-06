CREATE TABLE IF NOT EXISTS webux_users (
    id          INTEGER PRIMARY KEY,
    username    TEXT NOT NULL UNIQUE,
    password    TEXT NOT NULL,
    totp_secret TEXT,
    role        TEXT NOT NULL DEFAULT 'viewer',
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS webux_sessions (
    jti         TEXT PRIMARY KEY,
    user_id     INTEGER REFERENCES webux_users(id) ON DELETE CASCADE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS webux_audit_log (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER,
    method      TEXT NOT NULL,
    path        TEXT NOT NULL,
    body        TEXT,
    cli_echo    TEXT,
    result_code INTEGER,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS webux_cli_echo_log (
    id          INTEGER PRIMARY KEY,
    cmd         TEXT NOT NULL,
    explanation TEXT,
    context     TEXT,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS webux_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Port/socket scan snapshots for migration template generation
CREATE TABLE IF NOT EXISTS webux_port_snapshots (
    id          INTEGER PRIMARY KEY,
    snapshot    TEXT NOT NULL,  -- JSON blob of PortInfo slice
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Migration templates
CREATE TABLE IF NOT EXISTS webux_migration_templates (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    format      TEXT NOT NULL DEFAULT 'yaml',  -- yaml | markdown | ansible
    content     TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
