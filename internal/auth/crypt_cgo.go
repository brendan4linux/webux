//go:build cgo

package auth

// CGO is available but no longer needed for shadow auth —
// yescrypt, SHA-512, SHA-256, MD5 are all pure Go.
// CGO is unused; this file exists to avoid build tag conflicts.
func cryptPassword(password, setting string) (string, error) {
	return pureGoCrypt(password, setting)
}
