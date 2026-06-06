//go:build !pam

package auth

import "fmt"

// AuthenticatePAM is the stub used when built without -tags pam.
// Falls back to /etc/shadow verification using golang.org/x/crypto.
func AuthenticatePAM(username, password string) error {
	return authenticateShadow(username, password)
}

// pamAvailable reports whether PAM support is compiled in.
func pamAvailable() bool { return false }

// buildInfo returns the auth backend in use.
func buildInfo() string {
	return fmt.Sprintf("shadow (rebuild with -tags pam for full PAM support)")
}
