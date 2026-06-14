#!/bin/sh
# Post-remove hook — runs after package removal
# Data directory is intentionally preserved

if command -v systemctl >/dev/null 2>&1; then
  systemctl stop webux 2>/dev/null || true
  systemctl disable webux 2>/dev/null || true
  systemctl daemon-reload 2>/dev/null || true
elif [ -f /etc/init.d/webux ]; then
  /etc/init.d/webux stop 2>/dev/null || true
  if command -v update-rc.d >/dev/null 2>&1; then
    update-rc.d webux remove 2>/dev/null || true
  elif command -v chkconfig >/dev/null 2>&1; then
    chkconfig --del webux 2>/dev/null || true
  fi
fi

echo "Webux removed. Data preserved at /var/lib/webux"
