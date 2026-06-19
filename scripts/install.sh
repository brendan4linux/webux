#!/bin/sh
# Webux universal installer
# Usage: curl -fsSL https://example.com/install.sh | sudo sh
# Or:    sudo sh install.sh [--version v1.2.3] [--no-service]
set -e

REPO="brendan4linux/webux"
INSTALL_DIR="/usr/local/bin"
DATA_DIR="/var/lib/webux"
CONFIG_DIR="/etc/webux"
SERVICE_NAME="webux"
NO_SERVICE=0
VERSION=""

# ── Parse args ────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --no-service) NO_SERVICE=1; shift ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Colours ───────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'

info()    { printf "${BLUE}→${NC} %s\n" "$*"; }
success() { printf "${GREEN}✓${NC} %s\n" "$*"; }
warn()    { printf "${YELLOW}!${NC} %s\n" "$*"; }
die()     { printf "${RED}✗${NC} %s\n" "$*" >&2; exit 1; }

# ── Root check ────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
  die "This installer must be run as root (sudo sh install.sh)"
fi

printf "\n${BOLD}  Webux Linux Management Panel — Installer${NC}\n\n"

# ── Detect architecture ───────────────────────────────────
detect_arch() {
  arch=$(uname -m)
  case "$arch" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    armv7l|armv7)   echo "armv7" ;;
    armv6l)         echo "armv6" ;;
    i386|i686)      echo "386"   ;;
    *)              die "Unsupported architecture: $arch" ;;
  esac
}

ARCH=$(detect_arch)
info "Architecture: $ARCH"

# ── Detect OS ─────────────────────────────────────────────
detect_os() {
  if [ -f /etc/os-release ]; then
    . /etc/os-release
    echo "$ID"
  elif [ -f /etc/redhat-release ]; then
    echo "rhel"
  elif [ -f /etc/debian_version ]; then
    echo "debian"
  else
    echo "unknown"
  fi
}

OS=$(detect_os)
info "OS: $OS"

# ── Detect init system ────────────────────────────────────
detect_init() {
  if [ -d /run/systemd/system ]; then
    echo "systemd"
  elif [ -f /sbin/openrc ]; then
    echo "openrc"
  elif [ -f /etc/init.d/cron ] || [ -f /etc/init.d/crond ]; then
    echo "sysv"
  else
    echo "unknown"
  fi
}

INIT=$(detect_init)
info "Init system: $INIT"

# ── Detect download tool ──────────────────────────────────
if command -v curl >/dev/null 2>&1; then
  DOWNLOAD="curl -fsSL"
elif command -v wget >/dev/null 2>&1; then
  DOWNLOAD="wget -qO-"
else
  die "Neither curl nor wget found — install one and retry"
fi

# ── Resolve version ───────────────────────────────────────
if [ -z "$VERSION" ]; then
  info "Fetching latest version..."
  VERSION=$($DOWNLOAD "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')
  [ -z "$VERSION" ] && die "Could not determine latest version"
fi
info "Version: $VERSION"

# ── Download binary ───────────────────────────────────────
BINARY_NAME="webux-linux-${ARCH}"
DOWNLOAD_URL="https://github.com/$REPO/releases/download/$VERSION/${BINARY_NAME}.tar.gz"
TMPDIR=$(mktemp -d)
trap 'rm -rf $TMPDIR' EXIT

info "Downloading $BINARY_NAME..."
$DOWNLOAD "$DOWNLOAD_URL" > "$TMPDIR/webux.tar.gz" || \
  die "Download failed: $DOWNLOAD_URL"

tar -xzf "$TMPDIR/webux.tar.gz" -C "$TMPDIR"

# Find the binary in the extracted contents
BINARY=$(find "$TMPDIR" -name "webux" -type f | head -1)
[ -z "$BINARY" ] && BINARY=$(find "$TMPDIR" -name "${BINARY_NAME}" -type f | head -1)
[ -z "$BINARY" ] && die "Could not find webux binary in archive"

# ── Install binary ────────────────────────────────────────
info "Installing to $INSTALL_DIR/webux..."
install -m 755 "$BINARY" "$INSTALL_DIR/webux"
success "Binary installed"

# ── Create directories ────────────────────────────────────
mkdir -p "$DATA_DIR" "$CONFIG_DIR"
chmod 750 "$DATA_DIR" "$CONFIG_DIR"

# ── Install config (don't overwrite existing) ─────────────
if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
  cat > "$CONFIG_DIR/config.yaml" << CONFEOF
# Webux configuration
server:
  host: "0.0.0.0"
  port: 9090
data_dir: "$DATA_DIR"
log:
  level: "info"
CONFEOF
  success "Config written to $CONFIG_DIR/config.yaml"
else
  warn "Config already exists at $CONFIG_DIR/config.yaml — skipping"
fi

# ── Install service ───────────────────────────────────────
if [ "$NO_SERVICE" -eq 0 ]; then
  case "$INIT" in
    systemd)
      info "Installing systemd service..."
      cat > /etc/systemd/system/webux.service << SVCEOF
[Unit]
Description=Webux Linux Management Panel
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/webux
Restart=on-failure
RestartSec=5s
Environment=WEBUX_DATA_DIR=$DATA_DIR
Environment=WEBUX_CONFIG=$CONFIG_DIR/config.yaml
StandardOutput=journal
StandardError=journal
SyslogIdentifier=webux

[Install]
WantedBy=multi-user.target
SVCEOF
      systemctl daemon-reload
      systemctl enable webux
      systemctl start webux
      success "systemd service installed and started"
      ;;

    openrc)
      info "Installing OpenRC service..."
      cat > /etc/init.d/webux << RCEOF
#!/sbin/openrc-run
name="webux"
description="Webux Linux Management Panel"
command="$INSTALL_DIR/webux"
command_background=true
pidfile="/run/webux.pid"
export WEBUX_DATA_DIR=$DATA_DIR
export WEBUX_CONFIG=$CONFIG_DIR/config.yaml
RCEOF
      chmod +x /etc/init.d/webux
      rc-update add webux default
      rc-service webux start
      success "OpenRC service installed and started"
      ;;

    sysv)
      info "Installing SysV init script..."
      cat > /etc/init.d/webux << SVEOF
#!/bin/sh
### BEGIN INIT INFO
# Provides:          webux
# Required-Start:    \$network
# Required-Stop:     \$network
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: Webux Linux Management Panel
### END INIT INFO
NAME=webux
DAEMON=$INSTALL_DIR/webux
PIDFILE=/var/run/webux.pid
export WEBUX_DATA_DIR=$DATA_DIR
export WEBUX_CONFIG=$CONFIG_DIR/config.yaml
case "\$1" in
  start)
    start-stop-daemon --start --background --make-pidfile \
      --pidfile \$PIDFILE --exec \$DAEMON
    ;;
  stop)
    start-stop-daemon --stop --pidfile \$PIDFILE
    rm -f \$PIDFILE
    ;;
  restart) \$0 stop; sleep 1; \$0 start ;;
  status)
    [ -f \$PIDFILE ] && kill -0 \$(cat \$PIDFILE) 2>/dev/null && echo "running" && exit 0
    echo "stopped"; exit 1 ;;
  *) echo "Usage: \$0 {start|stop|restart|status}"; exit 1 ;;
esac
SVEOF
      chmod +x /etc/init.d/webux
      if command -v update-rc.d >/dev/null 2>&1; then
        update-rc.d webux defaults
      elif command -v chkconfig >/dev/null 2>&1; then
        chkconfig --add webux
        chkconfig webux on
      fi
      /etc/init.d/webux start
      success "SysV init script installed and started"
      ;;

    *)
      warn "Unknown init system — binary installed but no service configured"
      warn "Start manually: WEBUX_DATA_DIR=$DATA_DIR $INSTALL_DIR/webux"
      ;;
  esac
fi

# ── Done ──────────────────────────────────────────────────
printf "\n${GREEN}${BOLD}  Webux $VERSION installed successfully!${NC}\n"
printf "  Panel:  ${BOLD}http://$(hostname -I | awk '{print $1}'):9090${NC}\n"
printf "  Config: $CONFIG_DIR/config.yaml\n"
printf "  Data:   $DATA_DIR\n\n"
