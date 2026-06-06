CREATE TABLE IF NOT EXISTS webux_ai_providers (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    api_key     TEXT,
    base_url    TEXT,
    model       TEXT NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (datetime('now'))
);
