//go:build cgo

package auth

/*
#cgo LDFLAGS: -lcrypt
#define _GNU_SOURCE
#include <crypt.h>
#include <stdlib.h>
#include <string.h>

static char* do_crypt(const char* password, const char* setting) {
    struct crypt_data data;
    data.initialized = 0;
    char* result = crypt_r(password, setting, &data);
    if (result == NULL) return NULL;
    return strdup(result);
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// cryptPassword calls the system crypt_r(3) to hash a password with the
// given setting (which includes the algorithm prefix and salt).
// Handles yescrypt ($y$), SHA-512 ($6$), SHA-256 ($5$), MD5 ($1$), etc.
func cryptPassword(password, setting string) (string, error) {
	cPassword := C.CString(password)
	cSetting := C.CString(setting)
	defer C.free(unsafe.Pointer(cPassword))
	defer C.free(unsafe.Pointer(cSetting))

	result := C.do_crypt(cPassword, cSetting)
	if result == nil {
		return "", fmt.Errorf("crypt failed")
	}
	defer C.free(unsafe.Pointer(result))
	return C.GoString(result), nil
}
