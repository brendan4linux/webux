-- Migration 008: Security hardening score cache
INSERT OR IGNORE INTO webux_settings(key, value) VALUES ('hardening.results', '');
