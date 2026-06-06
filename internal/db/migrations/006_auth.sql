-- JWT secret — generated on first run if empty
INSERT OR IGNORE INTO webux_settings (key, value) VALUES
  ('auth.jwt_secret', ''),
  ('auth.bypass_token', ''),
  ('auth.session_ttl_hours', '24'),
  ('server.port', '8989');
