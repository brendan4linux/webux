package handlers

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// termUpgrader allows any origin — terminal is local-only admin tool.
var termUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

// TerminalHandler manages PTY sessions over WebSocket.
type TerminalHandler struct {
	db   *sql.DB
	mu   sync.Mutex
}

func NewTerminalHandler(db *sql.DB) *TerminalHandler {
	return &TerminalHandler{db: db}
}

// termMsg is the framing format for control messages from the browser.
// Data bytes are sent as raw binary WebSocket frames.
// Control messages are JSON text frames:
//
//	{"type":"resize","cols":220,"rows":50}
//	{"type":"run","cmd":"df -h\n"}      ← execute a command directly
type termMsg struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
	Cmd  string `json:"cmd"`
}

// ServeTerminal handles GET /ws/terminal
// Spawns a PTY, connects it to the WebSocket, cleans up on disconnect.
func (h *TerminalHandler) ServeTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("terminal ws upgrade", "err", err)
		return
	}
	defer conn.Close()

	shell := h.resolveShell()

	// Build the command — login shell behaviour
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	// Start with a PTY
	ptmx, err := pty.Start(cmd)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage,
			[]byte(fmt.Sprintf("\r\n\033[31mFailed to start shell: %v\033[0m\r\n", err)))
		return
	}
	defer func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Set initial size
	h.resizePTY(ptmx, 80, 24)

	// PTY → WebSocket (output)
	// When the shell exits, close the WebSocket so the client knows
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				// PTY closed — shell exited. Send a message then close the WS.
				conn.WriteMessage(websocket.TextMessage,
					[]byte("\r\n\x1b[33m[shell exited]\x1b[0m\r\n"))
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shell exited"))
				conn.Close()
				return
			}
		}
	}()

	// WebSocket → PTY (input + control messages)
	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if msgType == websocket.TextMessage {
			// Try to parse as a control message first
			var msg termMsg
			if json.Unmarshal(data, &msg) == nil && msg.Type != "" {
				switch msg.Type {
				case "resize":
					if msg.Cols > 0 && msg.Rows > 0 {
						h.resizePTY(ptmx, msg.Cols, msg.Rows)
					}
				case "run":
					if msg.Cmd != "" {
						cmd := msg.Cmd
						if !strings.HasSuffix(cmd, "\n") {
							cmd += "\n"
						}
						ptmx.Write([]byte(cmd))
					}
				}
			} else {
				// Not a control message — raw keystroke string, write directly to PTY
				ptmx.Write(data)
			}
		} else {
			// Binary/text input — raw keystrokes from xterm.js
			ptmx.Write(data)
		}
	}
}

// Settings handles GET /api/terminal/settings
func (h *TerminalHandler) Settings(w http.ResponseWriter, r *http.Request) {
	shell := h.resolveShell()
	configuredShell := h.getSetting("terminal.shell")
	quickCmds := h.getSetting("terminal.quick_commands")

	writeJSON(w, map[string]interface{}{
		"shell":            shell,
		"configured_shell": configuredShell,
		"quick_commands":   quickCmds, // raw JSON string — frontend parses
	})
}

// SaveSettings handles PUT /api/terminal/settings
func (h *TerminalHandler) SaveSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Shell         string `json:"shell"`
		QuickCommands string `json:"quick_commands"` // JSON string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.Shell != "" {
		h.setSetting("terminal.shell", body.Shell)
	}
	if body.QuickCommands != "" {
		h.setSetting("terminal.quick_commands", body.QuickCommands)
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

// resolveShell returns the shell to use, in preference order:
// 1. Configured override in settings
// 2. Current user's login shell from /etc/passwd
// 3. Fall back to /bin/bash → /bin/sh
func (h *TerminalHandler) resolveShell() string {
	if s := h.getSetting("terminal.shell"); s != "" {
		if _, err := exec.LookPath(s); err == nil {
			return s
		}
	}
	// Current user's shell from /etc/passwd
	if shell := currentUserShell(); shell != "" {
		return shell
	}
	// Fallbacks
	for _, sh := range []string{"/bin/bash", "/usr/bin/bash", "/bin/sh"} {
		if _, err := os.Stat(sh); err == nil {
			return sh
		}
	}
	return "/bin/sh"
}

func currentUserShell() string {
	uid := os.Getuid()
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 7 {
			continue
		}
		var entryUID int
		fmt.Sscanf(fields[2], "%d", &entryUID)
		if entryUID == uid {
			sh := strings.TrimSpace(fields[6])
			if sh != "" && sh != "/sbin/nologin" && sh != "/bin/false" {
				return sh
			}
		}
	}
	return ""
}

func (h *TerminalHandler) resizePTY(f *os.File, cols, rows uint16) {
	ws := struct {
		Row, Col, Xpixel, Ypixel uint16
	}{rows, cols, 0, 0}
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(),
		uintptr(syscall.TIOCSWINSZ), uintptr(unsafe.Pointer(&ws)))
}

func (h *TerminalHandler) getSetting(key string) string {
	if h.db == nil {
		return ""
	}
	var val string
	h.db.QueryRow("SELECT value FROM webux_settings WHERE key = ?", key).Scan(&val)
	return val
}

func (h *TerminalHandler) setSetting(key, val string) {
	if h.db == nil {
		return
	}
	h.db.Exec(`INSERT INTO webux_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`, key, val)
}
