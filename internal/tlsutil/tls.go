package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Config holds TLS configuration.
type Config struct {
	CertFile string // path to PEM cert — empty = auto-generate
	KeyFile  string // path to PEM key  — empty = auto-generate
	DataDir  string // where to store auto-generated cert/key
}

// Load returns a *tls.Config ready for use with http.Server.
// If CertFile and KeyFile are both set, they are loaded from disk.
// Otherwise a self-signed certificate is generated (and cached in DataDir).
func Load(cfg Config) (*tls.Config, error) {
	var cert tls.Certificate
	var err error

	if cfg.CertFile != "" && cfg.KeyFile != "" {
		// User-provided certificate
		cert, err = tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load TLS cert/key: %w", err)
		}
		slog.Info("TLS: loaded certificate from disk",
			"cert", cfg.CertFile,
			"key", cfg.KeyFile)
	} else {
		// Auto-generated self-signed certificate
		cert, err = loadOrGenerateSelfSigned(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("generate self-signed cert: %w", err)
		}
		slog.Info("TLS: using self-signed certificate",
			"cert", filepath.Join(cfg.DataDir, "webux.crt"),
			"key", filepath.Join(cfg.DataDir, "webux.key"))
		slog.Warn("TLS: self-signed cert — browsers will show a security warning. " +
			"Set tls_cert_file and tls_key_file in config.yaml to use your own certificate.")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		},
	}, nil
}

// loadOrGenerateSelfSigned loads a cached self-signed cert from dataDir,
// or generates a new one if it doesn't exist or is expiring within 30 days.
func loadOrGenerateSelfSigned(dataDir string) (tls.Certificate, error) {
	certPath := filepath.Join(dataDir, "webux.crt")
	keyPath  := filepath.Join(dataDir, "webux.key")

	// Try loading existing cert
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil {
				// Check expiry
				leaf, err := x509.ParseCertificate(cert.Certificate[0])
				if err == nil && time.Until(leaf.NotAfter) > 30*24*time.Hour {
					return cert, nil // still fresh
				}
				slog.Info("TLS: self-signed cert expiring soon, regenerating")
			}
		}
	}

	// Generate new self-signed cert
	return generateSelfSigned(certPath, keyPath)
}

// generateSelfSigned creates a new ECDSA P-256 self-signed certificate valid for 2 years.
func generateSelfSigned(certPath, keyPath string) (tls.Certificate, error) {
	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}

	// Certificate template
	hostname, _ := os.Hostname()
	template := &x509.Certificate{
		SerialNumber: newSerial(),
		Subject: pkix.Name{
			Organization: []string{"Webux"},
			CommonName:   hostname,
		},
		DNSNames: []string{
			hostname,
			"localhost",
		},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
		NotBefore:             time.Now().Add(-1 * time.Minute), // slight backdate for clock skew
		NotAfter:              time.Now().Add(2 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add local network IPs to the SAN list
	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok {
			if ip := ipnet.IP.To4(); ip != nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			} else if ip := ipnet.IP.To16(); ip != nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			}
		}
	}

	// Self-sign
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}

	// Write cert file
	certFile, err := os.OpenFile(certPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("write cert: %w", err)
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return tls.Certificate{}, err
	}

	// Write key file (mode 0600 — private!)
	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("write key: %w", err)
	}
	defer keyFile.Close()
	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}); err != nil {
		return tls.Certificate{}, err
	}

	slog.Info("TLS: generated new self-signed certificate",
		"cert", certPath, "key", keyPath,
		"valid_until", template.NotAfter.Format("2006-01-02"))

	return tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}),
	)
}

func newSerial() *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, _ := rand.Int(rand.Reader, max)
	return n
}
