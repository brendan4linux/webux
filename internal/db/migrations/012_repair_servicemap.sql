-- Repair: ensure sm_connection_stats exists (migration 011 may have
-- been applied before this table was part of the schema on some installs).
CREATE TABLE IF NOT EXISTS sm_connection_stats (
    id           INTEGER PRIMARY KEY,
    src_process  TEXT    NOT NULL DEFAULT '',
    dst_ip       TEXT    NOT NULL,
    dst_port     INTEGER NOT NULL,
    protocol     TEXT    NOT NULL DEFAULT 'tcp',
    service_hint TEXT    NOT NULL DEFAULT '',
    hit_count    INTEGER NOT NULL DEFAULT 0,
    first_seen   TEXT    NOT NULL DEFAULT (datetime('now')),
    last_seen    TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sm_conn
    ON sm_connection_stats(src_process, dst_ip, dst_port, protocol);
