//go:build !cgo

package auth

// cryptPassword implements crypt(3) in pure Go for the no-CGO build.
// Supports: yescrypt ($y$), SHA-512 ($6$), SHA-256 ($5$), MD5 ($1$), bcrypt ($2*$).
// This covers RHEL 6+, Ubuntu 16+, Debian 8+, Arch, and any other Linux distro.
func cryptPassword(password, setting string) (string, error) {
	return pureGoCrypt(password, setting)
}
