-- Ansible settings
INSERT OR IGNORE INTO webux_settings (key, value) VALUES
  ('ansible.playbook_dir', '/etc/ansible'),
  ('ansible.inventory',    '/etc/ansible/hosts');

-- AI / Ollama settings
INSERT OR IGNORE INTO webux_settings (key, value) VALUES
  ('ai.provider',      'ollama'),
  ('ai.ollama_url',    'http://localhost:11434'),
  ('ai.ollama_model',  ''),
  ('ai.system_prompt', 'You are a helpful Linux sysadmin assistant integrated into Webux, a server management panel. You have access to real-time system data including running services, processes, open ports, network interfaces, and more. Be concise, practical, and specific. When suggesting commands, prefer ones that work across distros.');
