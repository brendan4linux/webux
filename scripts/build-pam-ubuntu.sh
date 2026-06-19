#!/bin/sh
# build-pam-ubuntu.sh
# Builds a PAM-enabled webux binary inside an Ubuntu 24.04 container,
# producing a binary that runs on Ubuntu/Debian without library issues.
#
# Usage:
#   ./scripts/build-pam-ubuntu.sh           # uses git tag for version
#   ./scripts/build-pam-ubuntu.sh 0.9.6     # explicit version
#
# Requires: podman or docker
set -e
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --exact-match 2>/dev/null || git describe --tags 2>/dev/null | sed 's/^v//')}"
[ -z "$VERSION" ] && { echo "Error: no version — pass as argument or set a git tag"; exit 1; }
VERSION="$(echo "$VERSION" | sed 's/^v//')"

# Prefer podman, fall back to docker
if command -v podman >/dev/null 2>&1; then
    RUNTIME=podman
elif command -v docker >/dev/null 2>&1; then
    RUNTIME=docker
else
    echo "Error: neither podman nor docker found"; exit 1
fi

echo "→ Building webux-pam v${VERSION} for Ubuntu 24.04 using $RUNTIME"
echo "→ This will take a few minutes on first run (downloading Ubuntu image)"

mkdir -p build/release

$RUNTIME run --rm \
    -v "$(pwd):/build" \
    -w /build \
    ubuntu:24.04 \
    bash -c "
set -e
export DEBIAN_FRONTEND=noninteractive

echo '→ Installing build dependencies...'
apt-get update -qq
apt-get install -y -qq \
    golang git curl \
    libpam0g-dev libxcrypt-dev \
    build-essential ca-certificates \
    2>/dev/null

# Install Node.js 22
curl -fsSL https://deb.nodesource.com/setup_22.x | bash - >/dev/null 2>&1
apt-get install -y -qq nodejs 2>/dev/null

echo '→ Building frontend...'
cd web && npm ci --silent && cd ..

echo '→ Building webux-pam...'
go mod tidy
CGO_ENABLED=1 go build -tags 'mysql postgres pam' \
    -ldflags '-s -w -X main.version=${VERSION} -X main.commit=\$(git rev-parse --short HEAD 2>/dev/null || echo unknown) -X main.date=\$(date -u +%Y-%m-%dT%H:%M:%SZ)' \
    -trimpath \
    -o build/release/webux-pam-linux-amd64 \
    ./cmd/webux

echo '✓ Built: build/release/webux-pam-linux-amd64'
ldd build/release/webux-pam-linux-amd64
"

echo ""
echo "✓ Done — build/release/webux-pam-linux-amd64"
echo ""
echo "Next steps:"
echo "  ./scripts/build-packages.sh ${VERSION} amd64"
echo "  gh release upload v${VERSION} build/packages/*.deb build/packages/*.rpm"
