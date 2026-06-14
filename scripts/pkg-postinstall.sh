#!/bin/sh
# Post-install hook — runs after package installation
set -e

DATA_DIR="/var/lib/webux"
CONFIG_DIR="/etc/webux"

# Create data directory
mkdir -p "$DATA_DIR" "$CONFIG_DIR"
chmod 750 "$DATA_DIR"

# Enable and start service if systemd is available
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemctl daemon-reload
  systemctl enable webux 2>/dev/null || true
  systemctl start webux 2>/dev/null || true
  echo "Webux service started."
elif [ -f /etc/init.d/webux ]; then
  chmod +x /etc/init.d/webux
  if command -v update-rc.d >/dev/null 2>&1; then
    update-rc.d webux defaults 2>/dev/null || true
  elif command -v chkconfig >/dev/null 2>&1; then
    chkconfig --add webux 2>/dev/null || true
  fi
  /etc/init.d/webux start 2>/dev/null || true
fi

IP=$(hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost")
echo ""
echo "  Webux installed — panel available at http://${IP}:9090"
echo "  Config: $CONFIG_DIR/config.yaml"
echo ""
