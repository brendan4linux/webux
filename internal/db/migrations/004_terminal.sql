-- Terminal settings and quick commands
INSERT OR IGNORE INTO webux_settings (key, value) VALUES
  ('terminal.shell', ''),  -- empty = auto-detect from /etc/passwd
  ('terminal.quick_commands', '[
    {"label": "Disk usage",        "cmd": "df -h"},
    {"label": "Memory",            "cmd": "free -h"},
    {"label": "Top processes",     "cmd": "ps aux --sort=-%cpu | head -20"},
    {"label": "Active connections","cmd": "ss -tulpn"},
    {"label": "Last 50 auth logs", "cmd": "journalctl _COMM=sshd -n 50 --no-pager"},
    {"label": "Failed logins",     "cmd": "journalctl _COMM=sshd -n 100 --no-pager | grep -i fail"},
    {"label": "Systemd failed",    "cmd": "systemctl --failed"},
    {"label": "Kernel messages",   "cmd": "dmesg -T --level=err,warn | tail -30"},
    {"label": "CPU info",          "cmd": "lscpu"},
    {"label": "Block devices",     "cmd": "lsblk -o NAME,SIZE,TYPE,MOUNTPOINT,FSTYPE"},
    {"label": "Open files count",  "cmd": "lsof | wc -l"},
    {"label": "Uptime",            "cmd": "uptime -p"}
  ]');
