package svccheck

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunNginx runs security checks against the nginx configuration.
func RunNginx() []Check {
	cfgPath := findNginxConfig()
	cfgContent := ""
	if cfgPath != "" {
		b, _ := os.ReadFile(cfgPath)
		cfgContent = string(b)
		// Also read included conf.d files
		dir := filepath.Dir(cfgPath)
		for _, pat := range []string{
			filepath.Join(dir, "conf.d", "*.conf"),
			filepath.Join(dir, "sites-enabled", "*"),
		} {
			matches, _ := filepath.Glob(pat)
			for _, f := range matches {
				b2, _ := os.ReadFile(f)
				cfgContent += "\n" + string(b2)
			}
		}
	}

	return []Check{
		checkNginxServerTokens(cfgContent),
		checkNginxTLSVersion(cfgContent),
		checkNginxWeakCiphers(cfgContent),
		checkNginxSSLCert(cfgContent, cfgPath),
		checkNginxSecurityHeaders(cfgContent),
		checkNginxSessionCache(cfgContent),
	}
}

func findNginxConfig() string {
	candidates := []string{
		"/etc/nginx/nginx.conf",
		"/usr/local/etc/nginx/nginx.conf",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Ask nginx itself
	out, err := exec.Command("nginx", "-t", "-q").CombinedOutput()
	_ = out
	if err == nil {
		if out2, err2 := exec.Command("nginx", "-V").CombinedOutput(); err2 == nil {
			for _, f := range strings.Fields(string(out2)) {
				if strings.HasPrefix(f, "--conf-path=") {
					return strings.TrimPrefix(f, "--conf-path=")
				}
			}
		}
	}
	return ""
}

func checkNginxServerTokens(cfg string) Check {
	c := Check{
		ID:       "nginx_server_tokens",
		Label:    "Server version hidden (server_tokens off)",
		Severity: "medium",
		Fix:      "Add `server_tokens off;` inside the `http {}` block in nginx.conf",
	}
	if cfg == "" {
		c.Detail = "Could not read nginx config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "server_tokens off") {
		c.Pass = true
		c.Detail = "server_tokens off is set"
	} else {
		c.Detail = "server_tokens is not set to off — nginx version is disclosed in HTTP headers and error pages"
	}
	return c
}

func checkNginxTLSVersion(cfg string) Check {
	c := Check{
		ID:       "nginx_tls_version",
		Label:    "TLS 1.2+ only (no TLSv1/TLSv1.1)",
		Severity: "high",
		Fix:      "Set `ssl_protocols TLSv1.2 TLSv1.3;` in your nginx.conf ssl config",
	}
	if cfg == "" {
		c.Detail = "Could not read nginx config"
		return c
	}
	lower := strings.ToLower(cfg)
	if !strings.Contains(lower, "ssl_protocols") {
		// default in newer nginx is TLSv1 TLSv1.1 TLSv1.2 — not ideal
		c.Detail = "ssl_protocols not explicitly configured — nginx may allow TLSv1/TLSv1.1 by default"
		return c
	}
	if strings.Contains(lower, "tlsv1 ") || strings.Contains(lower, "tlsv1;") ||
		strings.Contains(lower, "tlsv1.1") || strings.Contains(lower, "sslv") {
		c.Detail = "ssl_protocols includes deprecated TLS versions (TLSv1 or TLSv1.1)"
		return c
	}
	c.Pass = true
	c.Detail = "ssl_protocols restricts to TLSv1.2/TLSv1.3"
	return c
}

func checkNginxWeakCiphers(cfg string) Check {
	c := Check{
		ID:       "nginx_weak_ciphers",
		Label:    "No weak ciphers (RC4, DES, MD5, NULL)",
		Severity: "high",
		Fix:      "Remove RC4, DES, 3DES, MD5, NULL, EXPORT, aNULL from ssl_ciphers",
	}
	if cfg == "" {
		c.Detail = "Could not read nginx config"
		return c
	}
	lower := strings.ToLower(cfg)
	if !strings.Contains(lower, "ssl_ciphers") {
		c.Pass = true
		c.Detail = "ssl_ciphers not set — using OpenSSL defaults (generally acceptable)"
		return c
	}
	weak := []string{"rc4", "+des", "3des", "+md5", "null", "export", "anull"}
	for _, w := range weak {
		if strings.Contains(lower, w) {
			c.Detail = "ssl_ciphers includes weak cipher: " + strings.ToUpper(w)
			return c
		}
	}
	c.Pass = true
	c.Detail = "No known weak ciphers found in ssl_ciphers"
	return c
}

func checkNginxSSLCert(cfg, cfgPath string) Check {
	c := Check{
		ID:       "nginx_ssl_cert",
		Label:    "SSL certificate is not self-signed",
		Severity: "high",
		Fix:      "Replace the self-signed certificate with one from a trusted CA (e.g. Let's Encrypt)",
	}
	certPath := extractNginxCertPath(cfg)
	if certPath == "" {
		c.Pass = true
		c.Detail = "No ssl_certificate directive found — SSL may not be configured"
		return c
	}
	b, err := os.ReadFile(certPath)
	if err != nil {
		c.Detail = "Could not read certificate at " + certPath + ": " + err.Error()
		return c
	}
	block, _ := pem.Decode(b)
	if block == nil {
		c.Detail = "Could not parse PEM certificate at " + certPath
		return c
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		c.Detail = "Could not parse certificate: " + err.Error()
		return c
	}
	if cert.Subject.String() == cert.Issuer.String() {
		c.Detail = "Certificate at " + certPath + " is self-signed (subject == issuer: " + cert.Subject.CommonName + ")"
		return c
	}
	c.Pass = true
	c.Detail = "Certificate issued by: " + cert.Issuer.CommonName
	return c
}

func extractNginxCertPath(cfg string) string {
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ssl_certificate ") && !strings.Contains(line, "ssl_certificate_key") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.TrimRight(parts[1], ";")
			}
		}
	}
	return ""
}

func checkNginxSecurityHeaders(cfg string) Check {
	c := Check{
		ID:       "nginx_security_headers",
		Label:    "Security headers configured",
		Severity: "medium",
		Fix:      "Add X-Frame-Options, X-Content-Type-Options, and Referrer-Policy headers to your nginx server blocks",
	}
	if cfg == "" {
		c.Detail = "Could not read nginx config"
		return c
	}
	lower := strings.ToLower(cfg)
	missing := []string{}
	headers := map[string]string{
		"x-frame-options":        "X-Frame-Options",
		"x-content-type-options": "X-Content-Type-Options",
		"referrer-policy":        "Referrer-Policy",
	}
	for key, name := range headers {
		if !strings.Contains(lower, key) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		c.Pass = true
		c.Detail = "X-Frame-Options, X-Content-Type-Options, Referrer-Policy all present"
	} else {
		c.Detail = "Missing security headers: " + strings.Join(missing, ", ")
	}
	return c
}

func checkNginxSessionCache(cfg string) Check {
	c := Check{
		ID:       "nginx_ssl_session_cache",
		Label:    "SSL session cache configured",
		Severity: "low",
		Fix:      "Add `ssl_session_cache shared:SSL:10m; ssl_session_timeout 1d;` to your nginx http or server block",
	}
	if cfg == "" {
		c.Detail = "Could not read nginx config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "ssl_session_cache") {
		c.Pass = true
		c.Detail = "ssl_session_cache is configured"
	} else {
		c.Detail = "ssl_session_cache not configured — SSL handshakes cannot be resumed, increasing server load"
	}
	return c
}
