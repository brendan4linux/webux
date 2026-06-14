-- Health check configuration
-- Stored as JSON array in webux_settings
-- Empty value = use DefaultChecks() from the health package
INSERT OR IGNORE INTO webux_settings (key, value) VALUES ('health.checks', '');
