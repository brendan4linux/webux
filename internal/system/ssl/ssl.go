// Package ssl scans, parses, and generates SSL/TLS certificates.
package ssl

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CertInfo describes a discovered certificate file.
type CertInfo struct {
	Path        string    `json:"path"`
	Source      string    `json:"source"` // "letsencrypt", "nginx", "apache", "system", "manual"
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	SelfSigned  bool      `json:"self_signed"`
	DNSNames    []string  `json:"dns_names,omitempty"`
	IPAddresses []string  `json:"ip_addresses,omitempty"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	DaysLeft    int       `json:"days_left"`
	Expired     bool      `json:"expired"`
	KeyType     string    `json:"key_type"`
	BitSize     int       `json:"bit_size,omitempty"`
}

// CSRRequest contains fields needed to generate a CSR + private key.
type CSRRequest struct {
	CommonName   string   `json:"common_name"`
	Organization string   `json:"organization"`
	Country      string   `json:"country"`
	State        string   `json:"state"`
	Locality     string   `json:"locality"`
	SANs         []string `json:"sans"` // DNS names and/or IPs
	KeyType      string   `json:"key_type"` // "rsa" or "ec"
	KeySize      int      `json:"key_size"` // 2048/4096 for RSA; 256/384 for EC curve bits
}

// CSRResult returns the generated PEM-encoded key and CSR.
type CSRResult struct {
	PrivateKeyPEM string `json:"private_key_pem"`
	CSRPEM        string `json:"csr_pem"`
}

// SelfSignedResult returns the generated PEM-encoded key and certificate.
type SelfSignedResult struct {
	PrivateKeyPEM string `json:"private_key_pem"`
	CertPEM       string `json:"cert_pem"`
}

// scanDirs holds directories and their source labels.
var scanDirs = []struct {
	path   string
	source string
	glob   string
}{
	{"/etc/letsencrypt/live", "letsencrypt", "*/fullchain.pem"},
	{"/etc/ssl/certs", "system", "*.pem"},
	{"/etc/ssl/certs", "system", "*.crt"},
	{"/etc/ssl/private", "system", "*.pem"},
	{"/etc/ssl/private", "system", "*.crt"},
	{"/etc/pki/tls/certs", "system", "*.pem"},
	{"/etc/pki/tls/certs", "system", "*.crt"},
	{"/etc/pki/ca-trust/source/anchors", "system", "*.pem"},
	{"/etc/nginx/ssl", "nginx", "*.pem"},
	{"/etc/nginx/ssl", "nginx", "*.crt"},
	{"/etc/apache2/ssl", "apache", "*.pem"},
	{"/etc/apache2/ssl", "apache", "*.crt"},
	{"/etc/httpd/ssl", "apache", "*.pem"},
	{"/etc/httpd/ssl", "apache", "*.crt"},
	{"/etc/haproxy", "haproxy", "*.pem"},
}

// webServerCertDirs scans nginx/apache config files for certificate paths.
var webServerConfigGlobs = []string{
	"/etc/nginx/sites-enabled/*",
	"/etc/nginx/conf.d/*.conf",
	"/etc/nginx/nginx.conf",
	"/etc/apache2/sites-enabled/*",
	"/etc/apache2/conf-enabled/*.conf",
	"/etc/httpd/conf.d/*.conf",
	"/etc/httpd/conf/httpd.conf",
}

var (
	nginxSSLCertRe  = regexp.MustCompile(`ssl_certificate\s+([^\s;]+)`)
	apacheSSLCertRe = regexp.MustCompile(`(?i)SSLCertificateFile\s+([^\s]+)`)
)

// ScanCerts discovers all certificate files on the system.
func ScanCerts() []CertInfo {
	seen := map[string]bool{}
	var certs []CertInfo

	// Scan static directories
	for _, d := range scanDirs {
		matches, _ := filepath.Glob(filepath.Join(d.path, d.glob))
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true
			info, err := ParseCertFile(path)
			if err != nil {
				continue
			}
			info.Source = d.source
			certs = append(certs, *info)
		}
	}

	// Scan web server configs for additional cert paths
	for _, glob := range webServerConfigGlobs {
		files, _ := filepath.Glob(glob)
		for _, configFile := range files {
			data, err := os.ReadFile(configFile)
			if err != nil {
				continue
			}
			content := string(data)
			for _, m := range nginxSSLCertRe.FindAllStringSubmatch(content, -1) {
				path := m[1]
				if seen[path] {
					continue
				}
				seen[path] = true
				info, err := ParseCertFile(path)
				if err != nil {
					continue
				}
				info.Source = "nginx"
				certs = append(certs, *info)
			}
			for _, m := range apacheSSLCertRe.FindAllStringSubmatch(content, -1) {
				path := m[1]
				if seen[path] {
					continue
				}
				seen[path] = true
				info, err := ParseCertFile(path)
				if err != nil {
					continue
				}
				info.Source = "apache"
				certs = append(certs, *info)
			}
		}
	}

	return certs
}

// ParseCertFile reads a PEM file and returns the first certificate's details.
func ParseCertFile(path string) (*CertInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	for {
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		return certToInfo(path, cert), nil
	}
	return nil, fmt.Errorf("no certificate found in %s", path)
}

func certToInfo(path string, cert *x509.Certificate) *CertInfo {
	now := time.Now()
	daysLeft := int(cert.NotAfter.Sub(now).Hours() / 24)

	keyType := "Unknown"
	bitSize := 0
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		keyType = "RSA"
		bitSize = pub.Size() * 8
	case *ecdsa.PublicKey:
		keyType = "EC"
		bitSize = pub.Params().BitSize
	}

	var ips []string
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	selfSigned := cert.Issuer.String() == cert.Subject.String()

	return &CertInfo{
		Path:        path,
		Subject:     cert.Subject.CommonName,
		Issuer:      cert.Issuer.CommonName,
		SelfSigned:  selfSigned,
		DNSNames:    cert.DNSNames,
		IPAddresses: ips,
		NotBefore:   cert.NotBefore,
		NotAfter:    cert.NotAfter,
		DaysLeft:    daysLeft,
		Expired:     now.After(cert.NotAfter),
		KeyType:     keyType,
		BitSize:     bitSize,
	}
}

// ReadCertPEM returns the raw PEM content of a cert file (certificates only, strips keys).
func ReadCertPEM(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	inCert := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "-----BEGIN CERTIFICATE-----" {
			inCert = true
		}
		if inCert {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
		if line == "-----END CERTIFICATE-----" {
			inCert = false
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("no certificate block found")
	}
	return sb.String(), nil
}

// GenerateCSR creates a private key and CSR in PEM format.
func GenerateCSR(req CSRRequest) (*CSRResult, error) {
	privKeyPEM, privKey, err := generateKey(req.KeyType, req.KeySize)
	if err != nil {
		return nil, err
	}

	subject := pkix.Name{
		CommonName:   req.CommonName,
		Organization: nonEmpty(req.Organization),
		Country:      nonEmpty(req.Country),
		Province:     nonEmpty(req.State),
		Locality:     nonEmpty(req.Locality),
	}

	tmpl := &x509.CertificateRequest{Subject: subject}
	for _, san := range req.SANs {
		san = strings.TrimSpace(san)
		if ip := net.ParseIP(san); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if san != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, san)
		}
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, tmpl, privKey)
	if err != nil {
		return nil, err
	}

	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes}))
	return &CSRResult{PrivateKeyPEM: privKeyPEM, CSRPEM: csrPEM}, nil
}

// GenerateSelfSigned creates a self-signed certificate + key.
func GenerateSelfSigned(req CSRRequest, days int) (*SelfSignedResult, error) {
	privKeyPEM, privKey, err := generateKey(req.KeyType, req.KeySize)
	if err != nil {
		return nil, err
	}

	pub := publicKey(privKey)
	if pub == nil {
		return nil, fmt.Errorf("could not extract public key")
	}

	subject := pkix.Name{
		CommonName:   req.CommonName,
		Organization: nonEmpty(req.Organization),
		Country:      nonEmpty(req.Country),
		Province:     nonEmpty(req.State),
		Locality:     nonEmpty(req.Locality),
	}

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               subject,
		NotBefore:             now,
		NotAfter:              now.AddDate(0, 0, days),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, san := range req.SANs {
		san = strings.TrimSpace(san)
		if ip := net.ParseIP(san); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if san != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, san)
		}
	}
	// Always include CN in SANs for modern clients
	if req.CommonName != "" {
		tmpl.DNSNames = append([]string{req.CommonName}, tmpl.DNSNames...)
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, privKey)
	if err != nil {
		return nil, err
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}))
	return &SelfSignedResult{PrivateKeyPEM: privKeyPEM, CertPEM: certPEM}, nil
}

func generateKey(keyType string, size int) (string, interface{}, error) {
	if keyType == "" {
		keyType = "rsa"
	}
	if size == 0 {
		if strings.ToLower(keyType) == "ec" {
			size = 256
		} else {
			size = 2048
		}
	}

	switch strings.ToLower(keyType) {
	case "ec", "ecdsa":
		var curve elliptic.Curve
		switch size {
		case 384:
			curve = elliptic.P384()
		case 521:
			curve = elliptic.P521()
		default:
			curve = elliptic.P256()
		}
		priv, err := ecdsa.GenerateKey(curve, rand.Reader)
		if err != nil {
			return "", nil, err
		}
		der, err := x509.MarshalECPrivateKey(priv)
		if err != nil {
			return "", nil, err
		}
		pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
		return pemStr, priv, nil
	default: // rsa
		if size != 4096 {
			size = 2048
		}
		priv, err := rsa.GenerateKey(rand.Reader, size)
		if err != nil {
			return "", nil, err
		}
		der := x509.MarshalPKCS1PrivateKey(priv)
		pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
		return pemStr, priv, nil
	}
}

func publicKey(priv interface{}) interface{} {
	switch k := priv.(type) {
	case *rsa.PrivateKey:
		return &k.PublicKey
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	}
	return nil
}

func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// TrustBackend describes how the OS manages trusted CA certs.
type TrustBackend struct {
	Dir     string
	Ext     string
	Command string // command to run after copying
	Args    []string
}

// DetectTrustBackend returns the system's CA trust backend, or nil if unknown.
func DetectTrustBackend() *TrustBackend {
	// Debian / Ubuntu
	if _, err := exec.LookPath("update-ca-certificates"); err == nil {
		return &TrustBackend{
			Dir:     "/usr/local/share/ca-certificates",
			Ext:     ".crt",
			Command: "update-ca-certificates",
		}
	}
	// RHEL / CentOS / Fedora / Arch (update-ca-trust)
	if _, err := exec.LookPath("update-ca-trust"); err == nil {
		return &TrustBackend{
			Dir:     "/etc/pki/ca-trust/source/anchors",
			Ext:     ".crt",
			Command: "update-ca-trust",
			Args:    []string{"extract"},
		}
	}
	// Arch trust (alternative path)
	if _, err := exec.LookPath("trust"); err == nil {
		return &TrustBackend{
			Dir:     "/etc/ca-certificates/trust-source/anchors",
			Ext:     ".crt",
			Command: "trust",
			Args:    []string{"extract-compat"},
		}
	}
	return nil
}

// InstallTrustedCert writes certPEM to the system trust store and runs the update command.
// name should be a simple identifier like "myapp" (no path separators, no extension).
func InstallTrustedCert(name, certPEM string) (string, error) {
	backend := DetectTrustBackend()
	if backend == nil {
		return "", fmt.Errorf("no supported CA trust backend found (tried update-ca-certificates, update-ca-trust, trust)")
	}

	// Sanitise the name
	safeName := regexp.MustCompile(`[^a-zA-Z0-9_\-]`).ReplaceAllString(name, "_")
	if safeName == "" {
		safeName = "webux_cert"
	}

	destPath := filepath.Join(backend.Dir, safeName+backend.Ext)

	if err := os.MkdirAll(backend.Dir, 0755); err != nil {
		return "", fmt.Errorf("create trust dir: %w", err)
	}
	if err := os.WriteFile(destPath, []byte(certPEM), 0644); err != nil {
		return "", fmt.Errorf("write cert: %w", err)
	}

	args := backend.Args
	cmd := exec.Command(backend.Command, args...)
	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, fmt.Errorf("%s failed: %w — %s", backend.Command, err, output)
	}
	return fmt.Sprintf("Installed to %s\n%s", destPath, output), nil
}
