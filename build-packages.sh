#!/bin/sh
# Webux package builder — .deb, .rpm, .pkg.tar.zst, .tar.gz for all arches
# Requires: fpm (gem install fpm)
#
# Usage:
#   ./scripts/build-packages.sh                  # version from git tag, all arches
#   ./scripts/build-packages.sh 0.9.6            # explicit version, all arches
#   ./scripts/build-packages.sh 0.9.6 amd64      # single arch
#
# Binary search order per arch (in ./build/release/):
#   webux-pam-linux-<arch>   ← PAM build (preferred)
#   webux-full-linux-<arch>  ← full build
#   webux-linux-<arch>       ← minimal build
#
set -e
cd "$(dirname "$0")/.."

# ── Colours ───────────────────────────────────────────────────────────────
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
    VERSION="$(git describe --tags --exact-match 2>/dev/null \
               || git describe --tags 2>/dev/null \
               || echo '')"
    [ -z "$VERSION" ] && die "No git tag found.\nRun: git tag v0.9.6  OR  $0 0.9.6"
fi
VERSION="$(echo "$VERSION" | sed 's/^v//')"
DEB_VERSION="$(echo "$VERSION" | sed 's/-/~/g')"
RPM_VERSION="$(echo "$VERSION" | sed 's/-[0-9]*-g[0-9a-f]*$//' | sed 's/-/./g')"

info "Version:     $VERSION"
info "Deb version: $DEB_VERSION"
info "RPM version: $RPM_VERSION"

# ── Architecture(s) ───────────────────────────────────────────────────────
ARCHES="amd64 arm64 armv7"
[ -n "$2" ] && ARCHES="$2"

RELEASE_DIR="./build/release"
OUT="./build/packages"
SCRIPTS="./scripts"
mkdir -p "$OUT"

# ── Prerequisites ─────────────────────────────────────────────────────────
sep "Checking prerequisites"
command -v fpm >/dev/null 2>&1 || die "fpm not found.\nInstall: gem install fpm"
success "fpm $(fpm --version 2>/dev/null)"
chmod +x "$SCRIPTS/pkg-postinstall.sh" "$SCRIPTS/pkg-postremove.sh" 2>/dev/null || true

HAS_PACMAN=false
fpm -s dir -t pacman --help >/dev/null 2>&1 && HAS_PACMAN=true
[ "$HAS_PACMAN" = "true" ] \
    && success "fpm pacman target available" \
    || warn "fpm pacman target unavailable — will generate PKGBUILD instead"

# ── Find binary for an arch ───────────────────────────────────────────────
find_binary() {
    _arch="$1"
    for name in \
        "$RELEASE_DIR/webux-pam-linux-${_arch}" \
        "$RELEASE_DIR/webux-full-linux-${_arch}" \
        "$RELEASE_DIR/webux-linux-${_arch}"; do
        [ -f "$name" ] && { echo "$name"; return 0; }
    done
    return 1
}

# ── Build one arch ────────────────────────────────────────────────────────
build_arch() {
    ARCH="$1"

    BINARY="$(find_binary "$ARCH")" || {
        warn "No binary found for $ARCH in $RELEASE_DIR/"
        warn "Run: make release-full  (or: make release-pam)"
        return 0
    }
    success "Binary: $BINARY ($(du -sh "$BINARY" | cut -f1))"

    case "$ARCH" in
        amd64)  DEB_ARCH=amd64;  RPM_ARCH=x86_64;  PACMAN_ARCH=x86_64 ;;
        arm64)  DEB_ARCH=arm64;  RPM_ARCH=aarch64;  PACMAN_ARCH=aarch64 ;;
        armv7)  DEB_ARCH=armhf;  RPM_ARCH=armv7hl;  PACMAN_ARCH=armv7h ;;
        386)    DEB_ARCH=i386;   RPM_ARCH=i686;      PACMAN_ARCH=i686 ;;
        *)      DEB_ARCH=$ARCH;  RPM_ARCH=$ARCH;     PACMAN_ARCH=$ARCH ;;
    esac

    sep "[$ARCH] Building packages"

    # ── .tar.gz ───────────────────────────────────────────────────────────
    TARBALL="$OUT/webux-${VERSION}-linux-${ARCH}.tar.gz"
    info "tar.gz → $TARBALL"
    fpm -s dir -t tar \
        --name webux --version "$VERSION" --prefix / \
        --package "$TARBALL" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/webux.service=/etc/systemd/system/webux.service" \
        "$SCRIPTS/webux.initd=/etc/init.d/webux" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
        "$SCRIPTS/install.sh=/usr/local/share/webux/install.sh"
    success "$(du -sh "$TARBALL" | cut -f1)  $TARBALL"

    # ── .deb ──────────────────────────────────────────────────────────────
    DEB="$OUT/webux_${DEB_VERSION}_${DEB_ARCH}.deb"
    info ".deb → $DEB"
    fpm -s dir -t deb \
        --name webux --version "$DEB_VERSION" \
        --architecture "$DEB_ARCH" \
        --description "Webux Linux Management Panel — zero-dependency web-based Linux admin panel" \
        --url "https://github.com/brendan4linux/webux" \
        --license "AGPL-3.0" \
        --maintainer "Webux <webux@example.com>" \
        --category admin \
        --depends "libxcrypt2 | libcrypt1 | libc6" \
        --deb-no-default-config-files \
        --deb-systemd "$SCRIPTS/webux.service" \
        --after-install "$SCRIPTS/pkg-postinstall.sh" \
        --after-remove  "$SCRIPTS/pkg-postremove.sh" \
        --package "$DEB" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
        || { warn ".deb build failed for $ARCH"; return 0; }
    success "$(du -sh "$DEB" | cut -f1)  $DEB"

    # ── .rpm ──────────────────────────────────────────────────────────────
    RPM="$OUT/webux-${RPM_VERSION}-1.${RPM_ARCH}.rpm"
    info ".rpm → $RPM"
    fpm -s dir -t rpm \
        --name webux --version "$RPM_VERSION" --iteration 1 \
        --architecture "$RPM_ARCH" \
        --description "Webux Linux Management Panel" \
        --url "https://github.com/brendan4linux/webux" \
        --license "AGPL-3.0" \
        --maintainer "Webux <webux@example.com>" \
        --category "Applications/System" \
        --depends "libxcrypt" \
        --after-install "$SCRIPTS/pkg-postinstall.sh" \
        --after-remove  "$SCRIPTS/pkg-postremove.sh" \
        --package "$RPM" --force --log error \
        "$BINARY=/usr/local/bin/webux" \
        "$SCRIPTS/webux.service=/usr/lib/systemd/system/webux.service" \
        "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
        || { warn ".rpm build failed for $ARCH"; return 0; }
    success "$(du -sh "$RPM" | cut -f1)  $RPM"

    # ── .pkg.tar.zst (Arch / Manjaro / CachyOS) ───────────────────────────
    if [ "$HAS_PACMAN" = "true" ]; then
        PACPKG="$OUT/webux-${VERSION}-1-${PACMAN_ARCH}.pkg.tar.zst"
        info ".pkg.tar.zst → $PACPKG"
        fpm -s dir -t pacman \
            --name webux --version "$VERSION" --iteration 1 \
            --architecture "$PACMAN_ARCH" \
            --description "Webux Linux Management Panel" \
            --url "https://github.com/brendan4linux/webux" \
            --license "AGPL-3.0" \
            --maintainer "Webux <webux@example.com>" \
            --depends "libxcrypt" \
            --after-install "$SCRIPTS/pkg-postinstall.sh" \
            --after-remove  "$SCRIPTS/pkg-postremove.sh" \
            --package "$PACPKG" --force --log error \
            "$BINARY=/usr/local/bin/webux" \
            "$SCRIPTS/webux.service=/usr/lib/systemd/system/webux.service" \
            "$SCRIPTS/config.yaml=/etc/webux/config.yaml" \
            || { warn ".pkg.tar.zst failed — building PKGBUILD instead"
                 build_pkgbuild "$ARCH" "$BINARY" "$PACMAN_ARCH"
                 return 0; }
        success "$(du -sh "$PACPKG" | cut -f1)  $PACPKG"
    else
        build_pkgbuild "$ARCH" "$BINARY" "$PACMAN_ARCH"
    fi
}

# ── PKGBUILD fallback ─────────────────────────────────────────────────────
build_pkgbuild() {
    _arch="$1"; _bin="$2"; _parch="$3"
    PKGDIR="$OUT/webux-pkgbuild-${_arch}"
    mkdir -p "$PKGDIR"
    cp "$_bin"                      "$PKGDIR/webux"
    cp "$SCRIPTS/webux.service"     "$PKGDIR/"
    cp "$SCRIPTS/config.yaml"       "$PKGDIR/"
    cp "$SCRIPTS/pkg-postinstall.sh" "$PKGDIR/post_install.sh"
    cp "$SCRIPTS/pkg-postremove.sh"  "$PKGDIR/post_remove.sh"
    SHA="$(sha256sum "$_bin" | awk '{print $1}')"
    VER="${VERSION//-/_}"
    cat > "$PKGDIR/PKGBUILD" << PKGEOF
# Maintainer: Webux <webux@example.com>
pkgname=webux
pkgver=${VER}
pkgrel=1
pkgdesc="Webux Linux Management Panel"
arch=('${_parch}')
url="https://github.com/brendan4linux/webux"
license=('AGPL3')
depends=('libxcrypt')
backup=('etc/webux/config.yaml')
source=("webux" "webux.service" "config.yaml" "post_install.sh" "post_remove.sh")
sha256sums=('${SHA}' 'SKIP' 'SKIP' 'SKIP' 'SKIP')

package() {
    install -Dm755 "\$srcdir/webux"         "\$pkgdir/usr/local/bin/webux"
    install -Dm644 "\$srcdir/webux.service" "\$pkgdir/usr/lib/systemd/system/webux.service"
    install -Dm644 "\$srcdir/config.yaml"   "\$pkgdir/etc/webux/config.yaml"
    install -dm750 "\$pkgdir/var/lib/webux"
}

post_install() { sh "\$srcdir/post_install.sh"; }
post_remove()  { sh "\$srcdir/post_remove.sh"; }
PKGEOF
    success "PKGBUILD → $PKGDIR/"
    info    "  To install on Arch: cd $PKGDIR && makepkg -si"
}

# ── Checksums ─────────────────────────────────────────────────────────────
write_checksums() {
    SUMFILE="$OUT/checksums-${VERSION}.sha256"
    ( cd "$OUT" && sha256sum *.deb *.rpm *.tar.gz *.zst 2>/dev/null ) > "$SUMFILE" || true
    success "Checksums → $SUMFILE"
}

# ── Main ──────────────────────────────────────────────────────────────────
sep "Webux Package Builder — v${VERSION}"

for ARCH in $ARCHES; do
    build_arch "$ARCH"
done

write_checksums

sep "Done — $OUT/"
printf "\n"
ls -lh "$OUT/"*.deb "$OUT/"*.rpm "$OUT/"*.tar.gz "$OUT/"*.zst 2>/dev/null \
    | awk '{printf "  %6s  %s\n", $5, $9}'
printf "\n${BOLD}Install:${NC}\n"
printf "  Debian/Ubuntu:  sudo dpkg -i $OUT/webux_${DEB_VERSION}_amd64.deb\n"
printf "  RHEL/Fedora:    sudo rpm -i  $OUT/webux-${RPM_VERSION}-1.x86_64.rpm\n"
printf "  Arch (pacman):  sudo pacman -U $OUT/webux-${VERSION}-1-x86_64.pkg.tar.zst\n"
printf "  Universal:      tar xzf $OUT/webux-${VERSION}-linux-amd64.tar.gz\n\n"
