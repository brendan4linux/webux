package auth

// sssd.go — SSSD authentication via PTY su(1) password injection.
//
// Flow:
//   1. Try /etc/shadow + pure-Go crypt (local users — yescrypt, SHA-512, etc.)
//   2. If user not in shadow, or SSSD is running, attempt su-based auth
//      which works with any SSSD backend: LDAP, AD, FreeIPA, Kerberos.
//
// No CGO, no libpam, no libcrypt. Zero native library dependencies.

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
)

// sssdAvailable returns true if SSSD is running on this system.
// Uses the D-Bus service name check via systemctl — no dbus CGO needed.
func sssdAvailable() bool {
	// Fast path: check the SSSD socket exists
	sockets := []string{
		"/var/lib/sss/pipes/nss",
		"/run/sss/pipe/nss",
		"/tmp/.sssd-pipe-nss",
	}
	for _, s := range sockets {
		if _, err := os.Stat(s); err == nil {
			return true
		}
	}
	// Slower path: check if sssd process is running
	if err := exec.Command("pgrep", "-x", "sssd").Run(); err == nil {
		return true
	}
	return false
}

// userInShadow returns true if the username has an entry in /etc/shadow
// with an actual password hash (not locked, not empty).
func userInShadow(username string) bool {
	f, err := os.Open("/etc/shadow")
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, username+":") {
			continue
		}
		fields := strings.SplitN(line, ":", 3)
		if len(fields) < 2 {
			return false
		}
		hash := fields[1]
		// Locked accounts start with ! or *, empty = no password
		if hash == "" || hash == "*" || strings.HasPrefix(hash, "!") {
			return false
		}
		return true
	}
	return false
}

// authenticateSU verifies username/password by spawning:
//   su -s /bin/sh -c 'exit 0' <username>
// and injecting the password via PTY. This works for:
//   - Local shadow accounts (fallback)
//   - SSSD: LDAP, Active Directory, FreeIPA, Kerberos
//   - Any PAM-configured auth source
//
// The PTY is needed because su reads the password from a terminal,
// not stdin — same reason sudo does it this way.
func authenticateSU(username, password string) error {
	// Build the su command
	// -s /bin/sh: use a known shell that always exists
	// -c 'exit 0': run a command that succeeds immediately if auth works
	cmd := exec.Command("su", "-s", "/bin/sh", username, "-c", "exit 0")

	// Allocate a PTY — su requires a terminal to prompt for password
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("su: failed to start: %w", err)
	}
	defer ptmx.Close()

	// Channel to collect output for error detection
	outputCh := make(chan string, 64)
	go func() {
		scanner := bufio.NewScanner(ptmx)
		for scanner.Scan() {
			line := scanner.Text()
			select {
			case outputCh <- line:
			default:
			}
		}
		close(outputCh)
	}()

	// Wait for the password prompt then inject the password.
	// su prints "Password:" to the PTY before reading.
	prompted := make(chan bool, 1)
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-outputCh:
				if !ok {
					prompted <- false
					return
				}
				lower := strings.ToLower(strings.TrimSpace(line))
				if strings.Contains(lower, "password") ||
					strings.Contains(lower, "passwort") || // German
					strings.HasSuffix(lower, ":") {
					prompted <- true
					return
				}
			case <-timer.C:
				prompted <- false
				return
			}
		}
	}()

	// Wait for prompt (or timeout)
	gotPrompt := <-prompted

	// Drain remaining output into a separate collector for error detection
	errLines := []string{}
	drainTimeout := time.NewTimer(200 * time.Millisecond)

	if gotPrompt {
		// Send password + newline to PTY
		_, err = fmt.Fprintf(ptmx, "%s\n", password)
		if err != nil {
			cmd.Process.Kill()
			return fmt.Errorf("authentication failed")
		}
		// Give su time to process
		drainTimeout.Reset(3 * time.Second)
	}

	// Collect remaining output
	go func() {
		for line := range outputCh {
			errLines = append(errLines, line)
		}
	}()
	<-drainTimeout.C

	// Wait for the process to exit
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			return nil // exit 0 — authenticated
		}
		// Check output for specific failure indicators
		combined := strings.ToLower(strings.Join(errLines, " "))
		if strings.Contains(combined, "incorrect") ||
			strings.Contains(combined, "denied") ||
			strings.Contains(combined, "failure") ||
			strings.Contains(combined, "invalid") {
			return fmt.Errorf("authentication failed")
		}
		return fmt.Errorf("authentication failed")
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		return fmt.Errorf("authentication timed out")
	}
}
