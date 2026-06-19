#!/bin/sh
# build-pam-rhel.sh
# Builds a PAM-enabled webux binary inside a RHEL UBI9 container,
# producing a binary that runs on RHEL 9, CentOS Stream 9, Fedora, Rocky, Alma.
#
# Usage:
#   ./scripts/build-pam-rhel.sh           # uses git tag for version
#   ./scripts/build-pam-rhel.sh 0.9.6     # explicit version
#
# Requires: podman or docker
set -e
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --exact-match 2>/dev/null || git describe --tags 2>/dev/null | sed 's/^v//')}"
[ -z "$VERSION" ] && { echo "Error: no version — pass as argument or set a git tag"; exit 1; }
VERSION="$(echo "$VERSION" | sed 's/^v//')"

if command -v podman >/dev/null 2>&1; then
    RUNTIME=podman
elif command -v docker >/dev/null 2>&1; then
    RUNTIME=docker
else
    echo "Error: neither podman nor docker found"; exit 1
fi

echo "→ Building webux-pam v${VERSION} for RHEL/UBI9 using $RUNTIME"
echo "→ This will take a few minutes on first run (downloading UBI9 image)"

mkdir -p build/release

# Use Red Hat UBI9 — freely redistributable, matches RHEL 9 / CentOS Stream 9 / Rocky / Alma
$RUNTIME run --rm \
    -v "$(pwd):/build" \
    -w /build \
    registry.access.redhat.com/ubi9/ubi:latest \
    bash -c "
set -e

echo '→ Installing build dependencies...'
dnf install -y -q \
    golang git curl \
    pam-devel libxcrypt-devel \
    gcc make ca-certificates \
    2>/dev/null

# Install Node.js 22 via NodeSource
curl -fsSL https://rpm.nodesource.com/setup_22.x | bash - >/dev/null 2>&1
dnf install -y -q nodejs 2>/dev/null

echo '→ Building frontend...'
cd web && npm ci --silent && cd ..

echo '→ Building webux-pam...'
go mod tidy
CGO_ENABLED=1 go build -tags 'mysql postgres pam' \
    -ldflags '-s -w -X main.version=${VERSION} -X main.commit=\$(git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.date=\$(date -u +%Y-%m-%dT%H:%M:%SZ)' \
    -trimpath \
    -o build/release/webux-pam-linux-amd64-rhel \
    ./cmd/webux

echo '✓ Built: build/release/webux-pam-linux-amd64-rhel'
ldd build/release/webux-pam-linux-amd64-rhel
"

echo ""
echo "✓ Done — build/release/webux-pam-linux-amd64-rhel"
echo ""
echo "Next steps:"
echo "  # Copy as the primary amd64 binary for RPM packaging:"
echo "  cp build/release/webux-pam-linux-amd64-rhel build/release/webux-pam-linux-amd64"
echo "  ./scripts/build-packages.sh ${VERSION} amd64"
echo "  gh release upload v${VERSION} build/packages/*.rpm"
