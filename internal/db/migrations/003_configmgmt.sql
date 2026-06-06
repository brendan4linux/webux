CREATE TABLE IF NOT EXISTS webux_ansible_runs (
    id           INTEGER PRIMARY KEY,
    playbook     TEXT NOT NULL,
    inventory    TEXT,
    triggered_by TEXT,
    output       TEXT,
    exit_code    INTEGER,
    started_at   TEXT,
    finished_at  TEXT
);

CREATE TABLE IF NOT EXISTS webux_puppet_cache (
    hostname     TEXT PRIMARY KEY,
    facts_json   TEXT,
    report_json  TEXT,
    refreshed_at TEXT NOT NULL DEFAULT (datetime('now'))
);
