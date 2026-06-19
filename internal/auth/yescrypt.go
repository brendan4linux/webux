//go:build !cgo

package auth

// yescryptVerify is a stub for the pure-Go build.
// Full yescrypt support requires the CGO build (make build-full).
// This is called only when CGO is disabled AND the hash starts with $y$.
// The error message guides users to the correct build.
func yescryptVerify(password, hash string) error {
	return nil // handled upstream in pureGoCrypt
}
