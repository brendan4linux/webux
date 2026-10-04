#!/bin/sh
# Post-install hook for webux packages
# DEB: $1 = "configure" (fresh) | "upgrade" (upgrade)
# RPM: $1 = 1 (fresh install) | 2 (upgrade)
# Arch: called via install() / upgrade() functions in .INSTALL — $1 is new ver, $2 is old ver

set -e

# Create data and config directories
mkdir -p /var/lib/webux
chmod 750 /var/lib/webux
mkdir -p /etc/webux

# Install default config if not already present
if [ ! -f /etc/webux/config.yaml ]; then
    cp /etc/webux/config.yaml.dpkg-new /etc/webux/config.yaml 2>/dev/null || true
fi

# Determine if this is a fresh install or an upgrade
IS_UPGRADE=false
case "$1" in
    # DEB: dpkg passes "configure" on fresh install and "upgrade" on upgrade
    upgrade)            IS_UPGRADE=true ;;
    # RPM: rpm passes 1 on install, 2 on upgrade
    2)                  IS_UPGRADE=true ;;
    # Arch .INSTALL upgrade() is called with two args; install() with one
    *) [ -n "$2" ] &&   IS_UPGRADE=true ;;
esac

# Read port from config for the post-install message
PORT=8989
if [ -f /etc/webux/config.yaml ]; then
    P=$(grep -E '^\s*port:' /etc/webux/config.yaml 2>/dev/null | head -1 | sed 's/.*port:[[:space:]]*//' | tr -d '"'"'"' ')
    [ -n "$P" ] && PORT="$P"
    A=$(grep -E '^\s*listen_addr:' /etc/webux/config.yaml 2>/dev/null | head -1 | sed 's/.*listen_addr:[[:space:]]*//' | tr -d '"'"'"': ')
    [ -n "$A" ] && PORT="$A"
fi

IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$IP" ] && IP="localhost"

# Manage the service — intentionally NOT checking is-system-running; it returns
# non-zero on degraded systems (common on Proxmox) even when systemd works fine.
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload 2>/dev/null || true
    systemctl enable webux 2>/dev/null || true

    if $IS_UPGRADE; then
        systemctl restart webux 2>/dev/null || true
        echo "Webux service restarted."
    else
        systemctl start webux 2>/dev/null || true
        echo "Webux service started."
    fi
elif [ -f /etc/init.d/webux ]; then
    chmod +x /etc/init.d/webux
    if $IS_UPGRADE; then
        /etc/init.d/webux restart 2>/dev/null || true
        echo "Webux service restarted."
    else
        /etc/init.d/webux start 2>/dev/null || true
        echo "Webux service started."
    fi
fi

if $IS_UPGRADE; then
    echo ""
    echo "  Webux upgraded — panel available at https://${IP}:${PORT}"
else
    echo ""
    echo "  Webux installed — panel available at https://${IP}:${PORT}"
    echo "  Config: /etc/webux/config.yaml"
    echo "  Note: self-signed cert — accept the browser security warning on first visit"
fi
echo ""
