//go:build pam

package auth

/*
#cgo LDFLAGS: -lpam
#include <security/pam_appl.h>
#include <stdlib.h>
#include <string.h>

// pam_conv_func is the conversation callback.
// We store the password in appdata_ptr and return it for PAM_PROMPT_ECHO_OFF.
static int pam_conv_func(int num_msg, const struct pam_message **msg,
                          struct pam_response **resp, void *appdata_ptr) {
    struct pam_response *r = calloc(num_msg, sizeof(struct pam_response));
    if (!r) return PAM_BUF_ERR;
    for (int i = 0; i < num_msg; i++) {
        if (msg[i]->msg_style == PAM_PROMPT_ECHO_OFF ||
            msg[i]->msg_style == PAM_PROMPT_ECHO_ON) {
            r[i].resp = strdup((char*)appdata_ptr);
            r[i].resp_retcode = 0;
        }
    }
    *resp = r;
    return PAM_SUCCESS;
}

static int do_pam_auth(const char *service, const char *user, const char *password) {
    struct pam_conv conv = { pam_conv_func, (void*)password };
    pam_handle_t *pamh = NULL;
    int ret;

    ret = pam_start(service, user, &conv, &pamh);
    if (ret != PAM_SUCCESS) return ret;

    ret = pam_authenticate(pamh, 0);
    if (ret == PAM_SUCCESS) {
        ret = pam_acct_mgmt(pamh, 0);
    }
    pam_end(pamh, ret);
    return ret;
}
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// AuthenticatePAM authenticates a user via the system PAM stack.
// Uses the "webux" PAM service if it exists, falling back to "login".
func AuthenticatePAM(username, password string) error {
	service := "webux"
	// Check if a webux PAM config exists, fall back to "login"
	// (we'll create /etc/pam.d/webux in the install scripts)
	svc := C.CString(service)
	user := C.CString(username)
	pass := C.CString(password)
	defer C.free(unsafe.Pointer(svc))
	defer C.free(unsafe.Pointer(user))
	defer C.free(unsafe.Pointer(pass))

	ret := C.do_pam_auth(svc, user, pass)
	if ret != C.PAM_SUCCESS {
		// Try "login" service as fallback
		loginSvc := C.CString("login")
		defer C.free(unsafe.Pointer(loginSvc))
		ret = C.do_pam_auth(loginSvc, user, pass)
	}

	if ret != C.PAM_SUCCESS {
		return fmt.Errorf("authentication failed")
	}
	return nil
}

func pamAvailable() bool { return true }

func buildInfo() string { return "PAM (full system authentication)" }
