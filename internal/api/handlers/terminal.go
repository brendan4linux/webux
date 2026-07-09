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
	osuser "os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/brendan4linux/webux/internal/auth"
)

var termUpgrader = websocket.Upgrader{
	CheckOrigin:     wsCheckOrigin,
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
}

func wsCheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	origin = strings.TrimPrefix(origin, "https://")
	origin = strings.TrimPrefix(origin, "http://")
	return origin == r.Host
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
// Spawns a PTY as the authenticated Webux user, connects it to the WebSocket, cleans up on disconnect.
func (h *TerminalHandler) ServeTerminal(w http.ResponseWriter, r *http.Request) {
	conn, err := termUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("terminal ws upgrade", "err", err)
		return
	}
	defer conn.Close()

	// Resolve the logged-in username from the JWT stored in context.
	username := ""
	if claims := auth.ClaimsFromContext(r.Context()); claims != nil {
		username = claims.Username
	}

	shell, sysProcAttr, env, dir := h.resolveUserContext(username)

	cmd := exec.Command(shell, "-l")
	cmd.Env = env
	cmd.Dir = dir
	if sysProcAttr != nil {
		cmd.SysProcAttr = sysProcAttr
	}

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
	username := ""
	if claims := auth.ClaimsFromContext(r.Context()); claims != nil {
		username = claims.Username
	}
	shell, _, _, _ := h.resolveUserContext(username)
	configuredShell := h.getSetting("terminal.shell")
	quickCmds := h.getSetting("terminal.quick_commands")

	writeJSON(w, map[string]interface{}{
		"shell":            shell,
		"configured_shell": configuredShell,
		"quick_commands":   quickCmds,
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

// resolveUserContext returns the shell, SysProcAttr, environment, and working directory
// to use for the terminal, scoped to the given username (the Webux login user).
//
// If webux runs as root, it will setuid/setgid to the target user.
// If the username is empty, "root", or lookup fails, it falls back to the process user.
func (h *TerminalHandler) resolveUserContext(username string) (shell string, attr *syscall.SysProcAttr, env []string, dir string) {
	// Shell override from settings (applies regardless of user)
	configuredShell := h.getSetting("terminal.shell")

	// Try to look up the OS user matching the Webux login
	var u *osuser.User
	if username != "" && username != "sso" {
		u, _ = osuser.Lookup(username)
	}
	if u == nil {
		// Fall back to the process's own user
		u, _ = osuser.Current()
	}

	// Resolve shell: settings override → user's login shell → fallback
	shell = configuredShell
	if shell == "" && u != nil {
		shell = userLoginShell(u.Username)
	}
	if shell == "" {
		for _, sh := range []string{"/bin/bash", "/usr/bin/bash", "/bin/sh"} {
			if _, err := os.Stat(sh); err == nil {
				shell = sh
				break
			}
		}
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	// Default to process working directory / home
	dir = "/"
	if u != nil && u.HomeDir != "" {
		dir = u.HomeDir
	}

	// Build a clean login environment for the user
	env = []string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"SHELL=" + shell,
	}
	if u != nil {
		env = append(env,
			"HOME="+u.HomeDir,
			"USER="+u.Username,
			"LOGNAME="+u.Username,
		)
	}

	// Set credentials if we're running as root and need to drop to a different user
	if u != nil && os.Getuid() == 0 {
		uid64, err1 := strconv.ParseUint(u.Uid, 10, 32)
		gid64, err2 := strconv.ParseUint(u.Gid, 10, 32)
		if err1 == nil && err2 == nil {
			// Collect supplementary groups
			groupIDs, _ := u.GroupIds()
			var gids []uint32
			for _, g := range groupIDs {
				if n, err := strconv.ParseUint(g, 10, 32); err == nil {
					gids = append(gids, uint32(n))
				}
			}
			attr = &syscall.SysProcAttr{
				Credential: &syscall.Credential{
					Uid:    uint32(uid64),
					Gid:    uint32(gid64),
					Groups: gids,
				},
				Setsid: true,
			}
		}
	}

	return shell, attr, env, dir
}

// userLoginShell reads /etc/passwd to find the login shell for the given username.
func userLoginShell(username string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), ":")
		if len(fields) < 7 || fields[0] != username {
			continue
		}
		sh := strings.TrimSpace(fields[6])
		if sh != "" && sh != "/sbin/nologin" && sh != "/bin/false" && sh != "/usr/sbin/nologin" {
			return sh
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
