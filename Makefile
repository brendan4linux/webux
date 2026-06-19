BINARY      := webux
CMD_DIR     := ./cmd/webux
WEB_DIR     := ./web
OUT_DIR     := ./build
PKG_DIR     := ./build/packages
SCRIPTS_DIR := ./scripts

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
# Strip leading 'v' — deb/rpm require versions starting with a digit (v1.0.0 → 1.0.0)
PKG_VERSION := $(VERSION:v%=%)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

# Build tags for DB drivers
TAGS        ?=

.PHONY: all build web dev clean install uninstall help \
        build-mysql build-postgres build-full \
        release release-amd64 release-arm64 release-armv7 release-386 \
        package package-amd64 package-arm64 package-armv7 \
        checksums

## all: Build frontend then backend (default)
all: web build

## web: Build the Svelte 5 frontend
web:
	@echo "→ Building frontend"
	cd $(WEB_DIR) && npm ci && npm run build
	@echo "→ Copying dist to cmd/webux/"
	rm -rf $(CMD_DIR)/dist
	cp -r $(WEB_DIR)/dist $(CMD_DIR)/dist
	@echo "✓ Frontend ready"

## build: Compile for current OS/arch
build: web
	@echo "→ Building $(BINARY) $(VERSION)"
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build \
		-tags "$(TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-trimpath \
		-o $(OUT_DIR)/$(BINARY) \
		$(CMD_DIR)
	@echo "✓ $(OUT_DIR)/$(BINARY)"

## build-mysql: Build with MySQL driver
build-mysql: web
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -tags mysql \
		-ldflags "$(LDFLAGS)" -trimpath \
		-o $(OUT_DIR)/$(BINARY)-mysql $(CMD_DIR)
	@echo "✓ $(OUT_DIR)/$(BINARY)-mysql"

## build-postgres: Build with PostgreSQL driver
build-postgres: web
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=0 go build -tags postgres \
		-ldflags "$(LDFLAGS)" -trimpath \
		-o $(OUT_DIR)/$(BINARY)-postgres $(CMD_DIR)
	@echo "✓ $(OUT_DIR)/$(BINARY)-postgres"

## build-full: Build with all DB drivers (CGO enabled for crypt/shadow auth)
build-full: web
	CGO_ENABLED=0 go build -tags "mysql postgres" \
		-ldflags "$(LDFLAGS)" -trimpath \
		-o $(OUT_DIR)/webux-full $(CMD_DIR)

## build-pam: Build with PAM auth + all DB drivers (requires CGO + libpam-dev)
## Debian/Ubuntu: apt install libpam0g-dev
## RHEL/Fedora:   dnf install pam-devel
## Arch:          pacman -S pam
build-pam: web
	@echo "→ Building $(BINARY) with PAM + all DB drivers"
	@mkdir -p $(OUT_DIR)
	CGO_ENABLED=1 go build -tags "mysql postgres pam" \
		-ldflags "$(LDFLAGS)" -trimpath \
		-o $(OUT_DIR)/$(BINARY)-pam $(CMD_DIR)
	@echo "✓ $(OUT_DIR)/$(BINARY)-pam (requires libpam on target system)"


# These skip the 'web' dependency — run 'make web' first once, then
# 'make release' to cross-compile all arches without rebuilding the frontend.

_build_cross:
	@mkdir -p $(OUT_DIR)/release
	@echo "  → $(GOOS)/$(GOARCH) $(GOARM) → $(OUT_BINARY)"
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) \
		go build \
		-tags "$(TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-trimpath \
		-o $(OUT_BINARY) \
		$(CMD_DIR)
	@echo "  ✓ $(OUT_BINARY)"

release-amd64:
	@$(MAKE) _build_cross \
		GOOS=linux GOARCH=amd64 GOARM= \
		OUT_BINARY=$(OUT_DIR)/release/$(BINARY)-linux-amd64

release-arm64:
	@$(MAKE) _build_cross \
		GOOS=linux GOARCH=arm64 GOARM= \
		OUT_BINARY=$(OUT_DIR)/release/$(BINARY)-linux-arm64

release-armv7:
	@$(MAKE) _build_cross \
		GOOS=linux GOARCH=arm GOARM=7 \
		OUT_BINARY=$(OUT_DIR)/release/$(BINARY)-linux-armv7

release-386:
	@$(MAKE) _build_cross \
		GOOS=linux GOARCH=386 GOARM= \
		OUT_BINARY=$(OUT_DIR)/release/$(BINARY)-linux-386

## release: Cross-compile for all architectures (run 'make web' first)
release: release-amd64 release-arm64 release-armv7 release-386
	@echo ""
	@echo "✓ Release binaries:"
	@ls -lh $(OUT_DIR)/release/
	@echo ""
	@echo "Next: make package   (requires fpm: gem install fpm)"

## release-full: Cross-compile with all DB drivers for all arches
release-full:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags "mysql postgres" \
		-ldflags "$(LDFLAGS)" -trimpath \
		-o $(OUT_DIR)/release/webux-full-linux-amd64 $(CMD_DIR)

## checksums: Generate SHA256 checksums for all release binaries
checksums:
	@cd $(OUT_DIR)/release && sha256sum $(BINARY)-linux-* > checksums.sha256
	@echo "✓ Checksums written to $(OUT_DIR)/release/checksums.sha256"
	@cat $(OUT_DIR)/release/checksums.sha256

# ── Packaging ─────────────────────────────────────────────────────────────────
# Requires fpm: gem install fpm
# Produces .deb, .rpm, .tar.gz for each architecture

# Helper: find binary for a given arch — accepts both plain and -full- variants
_find_binary = $(firstword $(wildcard 	$(OUT_DIR)/release/$(BINARY)-linux-$(1) 	$(OUT_DIR)/release/$(BINARY)-full-linux-$(1)))

package-amd64:
	@$(eval BIN := $(call _find_binary,amd64))
	@[ -n "$(BIN)" ] || { echo "No amd64 binary found — run 'make release' or 'make release-full' first"; exit 1; }
	@chmod +x $(SCRIPTS_DIR)/build-packages.sh $(SCRIPTS_DIR)/*.sh
	@sh $(SCRIPTS_DIR)/build-packages.sh $(PKG_VERSION) $(BIN) amd64

package-arm64:
	@$(eval BIN := $(call _find_binary,arm64))
	@[ -n "$(BIN)" ] || { echo "No arm64 binary found — run 'make release' or 'make release-full' first"; exit 1; }
	@sh $(SCRIPTS_DIR)/build-packages.sh $(PKG_VERSION) $(BIN) arm64

package-armv7:
	@$(eval BIN := $(call _find_binary,armv7))
	@[ -n "$(BIN)" ] || { echo "No armv7 binary found — run 'make release' or 'make release-full' first"; exit 1; }
	@sh $(SCRIPTS_DIR)/build-packages.sh $(PKG_VERSION) $(BIN) armv7

## package: Build .deb, .rpm, .tar.gz for all architectures
## (requires fpm: gem install fpm)
package: package-amd64 package-arm64 package-armv7
	@echo ""
	@echo "✓ Packages:"
	@ls -lh $(PKG_DIR)/

# ── Dev / local ───────────────────────────────────────────────────────────────

## dev: Run with live reload (requires 'air' and 'npm')
dev:
	@$(MAKE) -j2 dev-backend dev-frontend

dev-backend:
	air -c .air.toml

dev-frontend:
	cd $(WEB_DIR) && npm run dev

## install: Install binary + service to current system (requires root)
install: build
	install -m 755 $(OUT_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	mkdir -p /etc/webux /var/lib/webux
	[ -f /etc/webux/config.yaml ] || \
		install -m 644 $(SCRIPTS_DIR)/config.yaml /etc/webux/config.yaml
	install -m 644 $(SCRIPTS_DIR)/webux.service /etc/systemd/system/
	systemctl daemon-reload
	systemctl enable webux
	@echo "✓ Installed — run: systemctl start webux"

## uninstall: Remove from current system
uninstall:
	systemctl disable --now webux 2>/dev/null || true
	rm -f /usr/local/bin/$(BINARY) /etc/systemd/system/webux.service
	systemctl daemon-reload
	@echo "✓ Uninstalled (data preserved at /var/lib/webux)"

## clean: Remove all build artifacts
clean:
	rm -rf $(OUT_DIR) $(WEB_DIR)/dist $(CMD_DIR)/dist

## help: Show available targets
help:
	@echo ""
	@echo "  Webux Makefile — $(VERSION)"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
	@echo ""
	@echo "  Build tags: TAGS=\"mysql postgres\" make build"
	@echo "  Version override: VERSION=v1.2.3 make release"
	@echo ""
