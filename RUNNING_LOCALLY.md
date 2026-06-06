# Running Webux Locally

## Prerequisites

| Tool    | Version | Install |
|---------|---------|---------|
| Go      | ≥ 1.22  | https://go.dev/dl or `sudo apt install golang-go` |
| Node.js | ≥ 18    | https://nodejs.org or `nvm install 20` |

---

## First-time setup

```bash
# 1. Resolve Go dependencies
go mod tidy

# 2. Build frontend, copy dist, build binary — in one command:
make all

# 3. Run
mkdir -p /tmp/webux-data
WEBUX_DATA_DIR=/tmp/webux-data ./build/webux
```

Open **http://localhost:9090**

Run with `sudo` for full port→process mapping and service management:
```bash
sudo WEBUX_DATA_DIR=/tmp/webux-data ./build/webux
```

---

## Manual build steps (if not using make)

```bash
# 1. Resolve deps
go mod tidy

# 2. Build frontend
cd web && npm install && npm run build && cd ..

# 3. Copy dist next to embed.go (MUST happen before go build)
rm -rf cmd/webux/dist
cp -r web/dist cmd/webux/dist

# 4. Build Go binary
go build -o ./build/webux ./cmd/webux && echo "SUCCESS"

# 5. Run
mkdir -p /tmp/webux-data
WEBUX_DATA_DIR=/tmp/webux-data ./build/webux
```

---

## Known gotchas

**`go.mod` must have `go-sqlite3 v0.21.3`** — not v0.15.1.
If tidy fails, check: `grep sqlite3 go.mod`

**`pattern all:web/dist: no matching files found`**
The `cp -r web/dist cmd/webux/dist` step was skipped or the frontend
wasn't built yet. Run `make web` first.

**`"embed" imported and not used`**
The embed declaration lives in `cmd/webux/embed.go`. The `"embed"`
import must NOT appear in `cmd/webux/main.go`.

**Nested `cmd/webux/dist/dist/`**
The `cp` was run twice. Fix with:
```bash
rm -rf cmd/webux/dist && cp -r web/dist cmd/webux/dist
```

**Service management requires root**
The dbus system bus requires elevated privileges. Run with `sudo`.

**Processes show `—` for owner**
Reading `/proc/<pid>/fd` for other users' processes requires root.
