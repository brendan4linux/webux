#!/bin/sh
# Webux package builder — wraps fpm to produce .deb, .rpm, .tar.gz
# Requires: fpm (gem install fpm)
# Usage: ./scripts/build-packages.sh <version> <binary> <arch>
set -e

VERSION="${1:-0.1.0}"
BINARY="${2:-./build/webux}"
ARCH="${3:-amd64}"
OUT="./build/packages"
SCRIPTS="./scripts"

[ -f "$BINARY" ] || { echo "Binary not found: $BINARY"; exit 1; }
command -v fpm >/dev/null 2>&1 || { echo "fpm not found: gem install fpm"; exit 1; }

mkdir -p "$OUT"

# Ensure scripts are executable
chmod +x "$SCRIPTS/pkg-postinstall.sh" "$SCRIPTS/pkg-postremove.sh"

# Normalise arch names per package format
DEB_ARCH="$ARCH"
RPM_ARCH="$ARCH"
case "$ARCH" in
  amd64)  DEB_ARCH="amd64";  RPM_ARCH="x86_64"  ;;
  arm64)  DEB_ARCH="arm64";  RPM_ARCH="aarch64"  ;;
  armv7)  DEB_ARCH="armhf";  RPM_ARCH="armv7hl"  ;;
  386)    DEB_ARCH="i386";   RPM_ARCH="i686"      ;;
esac

echo "→ Packaging webux v${VERSION} for ${ARCH}"

# ── .tar.gz (universal) ────────────────────────────────────────────
TARBALL="$OUT/webux-${VERSION}-linux-${ARCH}.tar.gz"
echo "  building .tar.gz..."
fpm -s dir -t tar \
  --name webux \
  --version "$VERSION" \
  --prefix / \
  --package "$TARBALL" \
  "$BINARY=/usr/local/bin/webux" \
  "$SCRIPTS/webux.service=/etc/systemd/system/webux.service" \
  "$SCRIPTS/webux.initd=/etc/init.d/webux" \
  "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
  "$SCRIPTS/install.sh=/usr/local/share/webux/install.sh" \
  2>&1 | grep -v "^Created"
echo "  ✓ $TARBALL"

# ── .deb (Debian / Ubuntu 16+) ────────────────────────────────────
DEB="$OUT/webux_${VERSION}_${DEB_ARCH}.deb"
echo "  building .deb..."
fpm -s dir -t deb \
  --architecture "$DEB_ARCH" \
  --name webux \
  --version "$VERSION" \
  --description "Webux Linux Management Panel" \
  --long-description "Zero-dependency web-based Linux management panel. Single static binary with embedded SQLite and web UI." \
  --url "https://github.com/brendan4linux/webux" \
  --license "AGPL-3.0" \
  --maintainer "Webux <webux@example.com>" \
  --category admin \
  --depends "libxcrypt2 | libcrypt1 | libc6" \
  --deb-no-default-config-files \
  --deb-systemd "$SCRIPTS/webux.service" \
  --after-install "$SCRIPTS/pkg-postinstall.sh" \
  --after-remove "$SCRIPTS/pkg-postremove.sh" \
  --package "$DEB" \
  "$BINARY=/usr/local/bin/webux" \
  "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
  2>&1 | grep -v "^Created"
echo "  ✓ $DEB"

# ── .rpm (RHEL 7+ / Fedora / SUSE) ───────────────────────────────
RPM="$OUT/webux-${VERSION}-1.${RPM_ARCH}.rpm"
echo "  building .rpm..."
fpm -s dir -t rpm \
  --architecture "$RPM_ARCH" \
  --name webux \
  --version "$VERSION" \
  --description "Webux Linux Management Panel" \
  --url "https://github.com/brendan4linux/webux" \
  --license "AGPL-3.0" \
  --maintainer "Webux <webux@example.com>" \
  --category "Applications/System" \
  --depends "libxcrypt" \
  --after-install "$SCRIPTS/pkg-postinstall.sh" \
  --after-remove "$SCRIPTS/pkg-postremove.sh" \
  --package "$RPM" \
  "$BINARY=/usr/local/bin/webux" \
  "$SCRIPTS/webux.service=/usr/lib/systemd/system/webux.service" \
  "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
  2>&1 | grep -v "^Created"
echo "  ✓ $RPM"

echo ""
echo "Done. Packages written to $OUT/"
ls -lh "$OUT/"*"${ARCH}"* "$OUT/"*"${DEB_ARCH}"* "$OUT/"*"${RPM_ARCH}"* 2>/dev/null | sort -u
