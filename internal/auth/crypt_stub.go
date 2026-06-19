//go:build !cgo

package auth

func cryptPassword(password, setting string) (string, error) {
	return pureGoCrypt(password, setting)
}
