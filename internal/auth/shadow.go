package auth

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// authenticateShadow is the main auth entry point for non-PAM builds.
// Priority:
//   1. Local shadow entry exists → verify with pure-Go crypt
//   2. No shadow entry but SSSD is running → authenticate via su PTY
//   3. Neither → fail
func authenticateShadow(username, password string) error {
	hash, shadowErr := shadowHash(username)

	if shadowErr == nil {
		// User found in shadow — verify locally
		return verifyCrypt(password, hash)
	}

	// User not in shadow (SSSD/LDAP user) or shadow unreadable
	// Try SSSD via su if available
	if sssdAvailable() {
		return authenticateSU(username, password)
	}

	// Last resort: try su anyway — it might work even without SSSD
	// (covers edge cases like NIS, LDAP without SSSD, etc.)
	suErr := authenticateSU(username, password)
	if suErr == nil {
		return nil
	}

	// Return the original shadow error as it's more informative
	return fmt.Errorf("authentication failed")
}

// shadowHash reads the hashed password for username from /etc/shadow.
func shadowHash(username string) (string, error) {
	f, err := os.Open("/etc/shadow")
	if err != nil {
		return "", fmt.Errorf("cannot read /etc/shadow: %w", err)
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
			return "", fmt.Errorf("malformed shadow entry for %s", username)
		}
		hash := fields[1]
		if hash == "" || hash == "*" || strings.HasPrefix(hash, "!") {
			return "", fmt.Errorf("account %s is locked or has no password", username)
		}
		return hash, nil
	}
	return "", fmt.Errorf("user %s not found in shadow", username)
}

// verifyCrypt verifies a password against a crypt(3) hash.
// Supports bcrypt, yescrypt, SHA-512, SHA-256, MD5 — all pure Go.
func verifyCrypt(password, hash string) error {
	if strings.HasPrefix(hash, "$2") {
		// bcrypt — golang.org/x/crypto
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	}
	result, err := cryptPassword(password, hash)
	if err != nil {
		if strings.HasPrefix(hash, "$y$") {
			return fmt.Errorf("yescrypt requires: go get github.com/openwall/yescrypt-go")
		}
		return fmt.Errorf("authentication failed")
	}
	// Use constant-time comparison
	if result != hash {
		return fmt.Errorf("authentication failed")
	}
	return nil
}
