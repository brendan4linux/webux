package svccheck

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
)

// RunApache runs security checks against the Apache configuration.
func RunApache() []Check {
	cfg := loadApacheConfig()
	return []Check{
		checkApacheServerTokens(cfg),
		checkApacheServerSignature(cfg),
		checkApacheTLSVersion(cfg),
		checkApacheTraceEnable(cfg),
		checkApacheSSLCert(cfg),
	}
}

func loadApacheConfig() string {
	roots := []string{
		"/etc/apache2",
		"/etc/httpd",
		"/etc/httpd/conf",
		"/usr/local/etc/apache2",
	}
	var content strings.Builder
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		for _, pat := range []string{
			filepath.Join(root, "apache2.conf"),
			filepath.Join(root, "httpd.conf"),
			filepath.Join(root, "conf", "httpd.conf"),
			filepath.Join(root, "conf.d", "*.conf"),
			filepath.Join(root, "sites-enabled", "*.conf"),
			filepath.Join(root, "mods-enabled", "*.conf"),
		} {
			matches, _ := filepath.Glob(pat)
			for _, f := range matches {
				b, err := os.ReadFile(f)
				if err == nil {
					content.WriteString(string(b))
					content.WriteByte('\n')
				}
			}
		}
	}
	return content.String()
}

func checkApacheServerTokens(cfg string) Check {
	c := Check{
		ID:       "apache_server_tokens",
		Label:    "Server version hidden (ServerTokens Prod)",
		Severity: "medium",
		Fix:      "Set `ServerTokens Prod` in your Apache config to hide version details",
	}
	if cfg == "" {
		c.Detail = "Could not read Apache config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "servertokens prod") || strings.Contains(lower, "servertokens productonly") {
		c.Pass = true
		c.Detail = "ServerTokens is set to Prod/ProductOnly"
	} else {
		c.Detail = "ServerTokens is not set to Prod — Apache version and OS details may be exposed in HTTP headers"
	}
	return c
}

func checkApacheServerSignature(cfg string) Check {
	c := Check{
		ID:       "apache_server_signature",
		Label:    "Server signature disabled (ServerSignature Off)",
		Severity: "low",
		Fix:      "Set `ServerSignature Off` in your Apache config",
	}
	if cfg == "" {
		c.Detail = "Could not read Apache config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "serversignature off") {
		c.Pass = true
		c.Detail = "ServerSignature Off is set"
	} else {
		c.Detail = "ServerSignature is not Off — server version appears in error pages"
	}
	return c
}

func checkApacheTLSVersion(cfg string) Check {
	c := Check{
		ID:       "apache_tls_version",
		Label:    "TLS 1.2+ only (SSLProtocol)",
		Severity: "high",
		Fix:      "Set `SSLProtocol all -SSLv3 -TLSv1 -TLSv1.1` in your Apache SSL config",
	}
	if cfg == "" {
		c.Detail = "Could not read Apache config"
		return c
	}
	lower := strings.ToLower(cfg)
	if !strings.Contains(lower, "sslprotocol") {
		c.Detail = "SSLProtocol not configured — Apache may allow deprecated TLS versions"
		return c
	}
	// Check that TLSv1 and TLSv1.1 are explicitly excluded
	hasMinus1 := strings.Contains(lower, "-tlsv1 ") || strings.Contains(lower, "-tlsv1\n") || strings.Contains(lower, "-tlsv1\t")
	hasMinus11 := strings.Contains(lower, "-tlsv1.1")
	if hasMinus1 && hasMinus11 {
		c.Pass = true
		c.Detail = "SSLProtocol explicitly excludes TLSv1 and TLSv1.1"
	} else {
		c.Detail = "SSLProtocol may allow TLSv1 or TLSv1.1 — ensure both are excluded with `-TLSv1 -TLSv1.1`"
	}
	return c
}

func checkApacheTraceEnable(cfg string) Check {
	c := Check{
		ID:       "apache_trace_enable",
		Label:    "HTTP TRACE method disabled (TraceEnable Off)",
		Severity: "medium",
		Fix:      "Add `TraceEnable Off` to your Apache config",
	}
	if cfg == "" {
		c.Detail = "Could not read Apache config"
		return c
	}
	lower := strings.ToLower(cfg)
	if strings.Contains(lower, "traceenable off") {
		c.Pass = true
		c.Detail = "TraceEnable Off is set"
	} else {
		c.Detail = "TraceEnable is not Off — HTTP TRACE method is enabled, which can aid XSS attacks"
	}
	return c
}

func checkApacheSSLCert(cfg string) Check {
	c := Check{
		ID:       "apache_ssl_cert",
		Label:    "SSL certificate is not self-signed",
		Severity: "high",
		Fix:      "Replace the self-signed certificate with one from a trusted CA (e.g. Let's Encrypt)",
	}
	certPath := extractApacheCertPath(cfg)
	if certPath == "" {
		c.Pass = true
		c.Detail = "No SSLCertificateFile directive found — SSL may not be configured"
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
		c.Detail = "Certificate at " + certPath + " is self-signed (" + cert.Subject.CommonName + ")"
		return c
	}
	c.Pass = true
	c.Detail = "Certificate issued by: " + cert.Issuer.CommonName
	return c
}

func extractApacheCertPath(cfg string) string {
	for _, line := range strings.Split(cfg, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "sslcertificatefile ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}
