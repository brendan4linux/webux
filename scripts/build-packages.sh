#!/bin/sh
# Webux package builder
# Supports PAM builds (native arch only) + pure-Go for cross-compiled arches
#
# Usage:
#   ./scripts/build-packages.sh [version]
#   ./scripts/build-packages.sh 0.9.6
#
# Build flow:
#   make build-pam           → ./build/webux-pam          (local arch, PAM)
#   make release-full        → ./build/release/webux-full-linux-* (all arches, no PAM)
#   ./scripts/build-packages.sh 0.9.6
#
set -e
cd "$(dirname "$0")/.."

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'
info()    { printf "${BLUE}→${NC} %s\n" "$*"; }
success() { printf "${GREEN}✓${NC} %s\n" "$*"; }
warn()    { printf "${YELLOW}!${NC} %s\n" "$*"; }
die()     { printf "${RED}✗ ERROR:${NC} %s\n" "$*" >&2; exit 1; }
sep()     { printf "\n${BOLD}── %s ──${NC}\n" "$*"; }

# ── Version ───────────────────────────────────────────────────────────────
if [ -n "$1" ]; then
    VERSION="$1"
else
    VERSION="$(git describe --tags --exact-match 2>/dev/null || git describe --tags 2>/dev/null || echo '')"
    [ -z "$VERSION" ] && die "No git tag found. Pass version: $0 0.9.6"
fi
VERSION="$(echo "$VERSION" | sed 's/^v//')"
DEB_VERSION="$(echo "$VERSION" | sed 's/-/~/g')"
RPM_VERSION="$(echo "$VERSION" | sed 's/-[0-9]*-g[0-9a-f]*$//' | sed 's/-/./g')"

sep "Webux Package Builder v${VERSION}"
info "Deb: $DEB_VERSION  RPM: $RPM_VERSION"

# ── Detect local arch ─────────────────────────────────────────────────────
LOCAL_ARCH="$(uname -m)"
case "$LOCAL_ARCH" in
    x86_64)  LOCAL_ARCH=amd64 ;;
    aarch64) LOCAL_ARCH=arm64 ;;
    armv7l)  LOCAL_ARCH=armv7 ;;
    i686)    LOCAL_ARCH=386   ;;
esac
info "Local arch: $LOCAL_ARCH"

# ── Paths ─────────────────────────────────────────────────────────────────
BUILD="./build"
RELEASE="./build/release"
OUT="./build/packages"
SCRIPTS="./scripts"
mkdir -p "$OUT" "$RELEASE"
chmod +x "$SCRIPTS/pkg-postinstall.sh" "$SCRIPTS/pkg-postremove.sh" 2>/dev/null || true

# ── Prereqs ───────────────────────────────────────────────────────────────
sep "Checking prerequisites"
command -v fpm >/dev/null 2>&1 || die "fpm not found — gem install fpm"
success "fpm $(fpm --version 2>/dev/null)"

HAS_PACMAN=false
fpm -s dir -t pacman --help >/dev/null 2>&1 && HAS_PACMAN=true
[ "$HAS_PACMAN" = "true" ] && success "fpm pacman target available"

# ── Stage binaries ────────────────────────────────────────────────────────
# Priority: webux-pam (local arch) > webux-full (local arch) > webux (local arch)
# For cross-compiled arches: webux-full-linux-$arch from release/
sep "Staging binaries"

for ARCH in amd64 arm64 armv7; do
    if [ "$ARCH" = "$LOCAL_ARCH" ]; then
        # Local arch: prefer PAM build, fall back to full/base
        if [ -f "$BUILD/webux-pam" ]; then
            cp "$BUILD/webux-pam" "$RELEASE/webux-full-linux-$ARCH"
            success "[$ARCH] Using PAM build (webux-pam)"
        elif [ -f "$BUILD/webux-full" ]; then
            cp "$BUILD/webux-full" "$RELEASE/webux-full-linux-$ARCH"
            success "[$ARCH] Using full build (webux-full)"
        elif [ -f "$BUILD/webux" ]; then
            cp "$BUILD/webux" "$RELEASE/webux-full-linux-$ARCH"
            success "[$ARCH] Using base build (webux)"
        else
            warn "[$ARCH] No binary found in $BUILD/ — run: make build-pam"
        fi
    else
        # Cross-compiled arch: must exist in release/
        if [ -f "$RELEASE/webux-full-linux-$ARCH" ]; then
            success "[$ARCH] Found cross-compiled binary"
        else
            warn "[$ARCH] No cross-compiled binary — run: make release-full"
            warn "       Skipping $ARCH packages"
        fi
    fi
done

# ── Package function ──────────────────────────────────────────────────────
package_arch() {
    ARCH="$1"
    BINARY="$RELEASE/webux-full-linux-$ARCH"

    [ -f "$BINARY" ] || { warn "Skipping $ARCH — no binary"; return 0; }
    success "Packaging $ARCH ($(du -sh "$BINARY" | cut -f1))"

    case "$ARCH" in
        amd64) DEB_ARCH=amd64;  RPM_ARCH=x86_64;  PACMAN_ARCH=x86_64 ;;
        arm64) DEB_ARCH=arm64;  RPM_ARCH=aarch64;  PACMAN_ARCH=aarch64 ;;
        armv7) DEB_ARCH=armhf;  RPM_ARCH=armv7hl;  PACMAN_ARCH=armv7h ;;
        *)     DEB_ARCH=$ARCH;  RPM_ARCH=$ARCH;     PACMAN_ARCH=$ARCH ;;
    esac

    sep "[$ARCH] Building packages"

    # .tar.gz
    TARBALL="$OUT/webux-${VERSION}-linux-${ARCH}.tar.gz"
    info "tar.gz → $TARBALL"
    fpm -s dir -t tar --name webux --version "$VERSION" --prefix / \
        --package "$TARBALL" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/webux.service=/etc/systemd/system/webux.service" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
        "$SCRIPTS/install.sh=/usr/local/share/webux/install.sh"
    success "$(du -sh "$TARBALL" | cut -f1)  $TARBALL"

    # .deb
    DEB="$OUT/webux_${DEB_VERSION}_${DEB_ARCH}.deb"
    info ".deb → $DEB"
    fpm -s dir -t deb \
        --name webux --version "$DEB_VERSION" \
        --architecture "$DEB_ARCH" \
        --description "Webux Linux Management Panel — zero-dependency web-based Linux admin panel" \
        --url "https://github.com/brendan4linux/webux" \
        --license "AGPL-3.0" \
        --maintainer "Brendan <brendan4linux@github.com>" \
        --category admin \
        --depends "libxcrypt2 | libcrypt1 | libc6" \
        --deb-no-default-config-files \
        --deb-systemd "$SCRIPTS/webux.service" \
        --after-install "$SCRIPTS/pkg-postinstall.sh" \
        --after-remove  "$SCRIPTS/pkg-postremove.sh" \
        --package "$DEB" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" || {
            warn ".deb failed for $ARCH"; return 0; }
    success "$(du -sh "$DEB" | cut -f1)  $DEB"

    # .rpm
    RPM="$OUT/webux-${RPM_VERSION}-1.${RPM_ARCH}.rpm"
    info ".rpm → $RPM"
    fpm -s dir -t rpm \
        --name webux --version "$RPM_VERSION" --iteration 1 \
        --architecture "$RPM_ARCH" \
        --description "Webux Linux Management Panel" \
        --url "https://github.com/brendan4linux/webux" \
        --license "AGPL-3.0" \
        --maintainer "Brendan <brendan4linux@github.com>" \
        --category "Applications/System" \
        --depends "libxcrypt" \
        --after-install "$SCRIPTS/pkg-postinstall.sh" \
        --after-remove  "$SCRIPTS/pkg-postremove.sh" \
        --package "$RPM" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/webux.service=/usr/lib/systemd/system/webux.service" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" || {
            warn ".rpm failed for $ARCH"; return 0; }
    success "$(du -sh "$RPM" | cut -f1)  $RPM"

    # .pkg.tar.zst (Arch)
    if [ "$HAS_PACMAN" = "true" ]; then
        PACPKG="$OUT/webux-${VERSION}-1-${PACMAN_ARCH}.pkg.tar.zst"
        info ".pkg.tar.zst → $PACPKG"
        fpm -s dir -t pacman \
            --name webux --version "$VERSION" --iteration 1 \
            --architecture "$PACMAN_ARCH" \
            --description "Webux Linux Management Panel" \
            --url "https://github.com/brendan4linux/webux" \
            --license "AGPL-3.0" \
            --depends "libxcrypt" \
            --after-install "$SCRIPTS/pkg-postinstall.sh" \
            --after-remove  "$SCRIPTS/pkg-postremove.sh" \
            --package "$PACPKG" --force --log error \
            "$BINARY=/usr/local/bin/webux" \
            "$SCRIPTS/webux.service=/usr/lib/systemd/system/webux.service" \
            "$SCRIPTS/config.yaml=/etc/webux/config.yaml" || {
                warn ".pkg.tar.zst failed for $ARCH"; return 0; }
        success "$(du -sh "$PACPKG" | cut -f1)  $PACPKG"
    fi
}

# ── Build all arches ──────────────────────────────────────────────────────
for ARCH in amd64 arm64 armv7; do
    package_arch "$ARCH"
done

# ── Checksums ─────────────────────────────────────────────────────────────
SUMFILE="$OUT/checksums-${VERSION}.sha256"
(cd "$OUT" && sha256sum *.deb *.rpm *.tar.gz *.zst 2>/dev/null) > "$SUMFILE" || true
success "Checksums → $SUMFILE"

sep "Done — $OUT/"
ls -lh "$OUT/"*.deb "$OUT/"*.rpm "$OUT/"*.tar.gz "$OUT/"*.zst 2>/dev/null \
    | awk '{printf "  %6s  %s\n", $5, $9}'

printf "\n${BOLD}Full workflow:${NC}\n"
printf "  make build-pam      # PAM binary for local arch ($LOCAL_ARCH)\n"
printf "  make release-full   # Pure-Go binaries for all other arches\n"
printf "  $0 $VERSION\n\n"
