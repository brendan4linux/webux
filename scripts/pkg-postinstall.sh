#!/bin/sh
# Post-install hook for webux packages

# Create data directory
mkdir -p /var/lib/webux
chmod 750 /var/lib/webux

# Create config directory if needed
mkdir -p /etc/webux

# Install default config if not already present
if [ ! -f /etc/webux/config.yaml ]; then
    cp /etc/webux/config.yaml.dpkg-new /etc/webux/config.yaml 2>/dev/null || true
fi

# Read port from config (default 8989)
PORT=8989
if [ -f /etc/webux/config.yaml ]; then
    CONFIGURED=$(grep -E '^\s*port:' /etc/webux/config.yaml 2>/dev/null | head -1 | sed 's/.*port:\s*//' | tr -d '"'"'"' ')
    [ -n "$CONFIGURED" ] && PORT="$CONFIGURED"
    # Also check listen_addr format ":8989"
    ADDR=$(grep -E '^\s*listen_addr:' /etc/webux/config.yaml 2>/dev/null | head -1 | sed 's/.*listen_addr:\s*//' | tr -d '"'"'"': ')
    [ -n "$ADDR" ] && PORT="$ADDR"
fi

# Get primary IP
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$IP" ] && IP="localhost"

# Enable and start service
if command -v systemctl >/dev/null 2>&1 && systemctl is-system-running >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
    systemctl enable webux 2>/dev/null || true
    systemctl restart webux 2>/dev/null || true
    echo "Webux service started."
elif [ -f /etc/init.d/webux ]; then
    chmod +x /etc/init.d/webux
    /etc/init.d/webux start 2>/dev/null || true
    echo "Webux service started."
fi

echo ""
echo "  Webux installed — panel available at https://${IP}:${PORT}"
echo "  Config: /etc/webux/config.yaml"
echo "  Note: self-signed cert — accept the browser security warning on first visit"
echo ""
