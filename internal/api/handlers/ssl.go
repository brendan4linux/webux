package handlers

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/brendan4linux/webux/internal/system/ssl"
)

type SSLHandler struct{}

func NewSSLHandler() *SSLHandler { return &SSLHandler{} }

// List handles GET /api/ssl/certs
func (h *SSLHandler) List(w http.ResponseWriter, r *http.Request) {
	certs := ssl.ScanCerts()
	writeJSON(w, certs)
}

// Get handles GET /api/ssl/cert?path=...
func (h *SSLHandler) Get(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if !safeCertPath(path) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	info, err := ssl.ParseCertFile(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, info)
}

// PEM handles GET /api/ssl/cert/pem?path=...
func (h *SSLHandler) PEM(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if !safeCertPath(path) {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	raw, err := ssl.ReadCertPEM(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"pem": raw})
}

// GenerateCSR handles POST /api/ssl/csr
func (h *SSLHandler) GenerateCSR(w http.ResponseWriter, r *http.Request) {
	var req ssl.CSRRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.CommonName == "" {
		http.Error(w, "common_name is required", http.StatusBadRequest)
		return
	}
	result, err := ssl.GenerateCSR(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// GenerateSelfSigned handles POST /api/ssl/self-signed
func (h *SSLHandler) GenerateSelfSigned(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ssl.CSRRequest
		Days        int    `json:"days"`
		Install     bool   `json:"install"`
		InstallName string `json:"install_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if body.CommonName == "" {
		http.Error(w, "common_name is required", http.StatusBadRequest)
		return
	}
	if body.Days <= 0 {
		body.Days = 365
	}
	result, err := ssl.GenerateSelfSigned(body.CSRRequest, body.Days)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type response struct {
		ssl.SelfSignedResult
		InstallOutput string `json:"install_output,omitempty"`
		InstallError  string `json:"install_error,omitempty"`
	}
	resp := response{SelfSignedResult: *result}

	if body.Install {
		name := body.InstallName
		if name == "" {
			name = body.CommonName
		}
		out, installErr := ssl.InstallTrustedCert(name, result.CertPEM)
		if installErr != nil {
			resp.InstallError = installErr.Error()
		} else {
			resp.InstallOutput = out
		}
	}

	writeJSON(w, resp)
}

// safeCertPath rejects any path that doesn't look like a real cert location.
func safeCertPath(path string) bool {
	if path == "" {
		return false
	}
	// must be absolute, no traversal
	if !filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return false
	}
	// must be under a known cert location
	allowed := []string{
		"/etc/ssl/", "/etc/letsencrypt/", "/etc/nginx/", "/etc/apache2/",
		"/etc/httpd/", "/etc/pki/", "/etc/haproxy/", "/var/lib/",
	}
	for _, prefix := range allowed {
		if strings.HasPrefix(clean, prefix) {
			return true
		}
	}
	return false
}
