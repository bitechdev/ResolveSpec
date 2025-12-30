package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

// certGenerationMutex protects concurrent certificate generation for the same host
var certGenerationMutex sync.Mutex

// generateSelfSignedCert generates a self-signed certificate for the given host.
// Returns the certificate and private key in PEM format.
func generateSelfSignedCert(host string) (certPEM, keyPEM []byte, err error) {
	// Generate private key
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	notBefore := time.Now()
	notAfter := notBefore.Add(365 * 24 * time.Hour) // Valid for 1 year

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"ResolveSpec Self-Signed"},
			CommonName:   host,
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add host as DNS name or IP address
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}

	// Create certificate
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Encode certificate to PEM
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	// Encode private key to PEM
	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	return certPEM, keyPEM, nil
}

// sanitizeHostname converts a hostname to a safe filename by replacing invalid characters.
func sanitizeHostname(host string) string {
	// Replace any character that's not alphanumeric, dot, or dash with underscore
	safe := ""
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			safe += string(r)
		} else {
			safe += "_"
		}
	}
	return safe
}

// getCertDirectory returns the directory path for storing self-signed certificates.
// Creates the directory if it doesn't exist.
func getCertDirectory() (string, error) {
	// Use a consistent directory in the user's cache directory
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// Fallback to current directory if cache dir is not available
		cacheDir = "."
	}
	
	certDir := filepath.Join(cacheDir, "resolvespec", "certs")
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create certificate directory: %w", err)
	}
	
	return certDir, nil
}

// isCertificateValid checks if a certificate file exists and is not expired.
func isCertificateValid(certFile string) bool {
	// Check if file exists
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return false
	}
	
	// Parse certificate
	block, _ := pem.Decode(certData)
	if block == nil {
		return false
	}
	
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	
	// Check if certificate is expired or will expire in the next 30 days
	now := time.Now()
	expiryThreshold := now.Add(30 * 24 * time.Hour)
	
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return false
	}
	
	// Renew if expiring soon
	if expiryThreshold.After(cert.NotAfter) {
		return false
	}
	
	return true
}

// saveCertToFiles saves certificate and key PEM data to persistent files.
// Returns the file paths for the certificate and key.
func saveCertToFiles(certPEM, keyPEM []byte, host string) (certFile, keyFile string, err error) {
	// Get certificate directory
	certDir, err := getCertDirectory()
	if err != nil {
		return "", "", err
	}
	
	// Sanitize hostname for safe file naming
	safeHost := sanitizeHostname(host)
	
	// Use consistent file names based on host
	certFile = filepath.Join(certDir, fmt.Sprintf("%s-cert.pem", safeHost))
	keyFile = filepath.Join(certDir, fmt.Sprintf("%s-key.pem", safeHost))
	
	// Write certificate
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write certificate: %w", err)
	}
	
	// Write key
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write private key: %w", err)
	}
	
	return certFile, keyFile, nil
}

// setupAutoTLS configures automatic TLS certificate management using Let's Encrypt.
// Returns a TLS config that can be used with http.Server.
func setupAutoTLS(domains []string, email, cacheDir string) (*tls.Config, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("at least one domain must be specified for AutoTLS")
	}

	// Set default cache directory
	if cacheDir == "" {
		cacheDir = "./certs-cache"
	}

	// Create cache directory if it doesn't exist
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate cache directory: %w", err)
	}

	// Create autocert manager
	m := &autocert.Manager{
		Prompt:      autocert.AcceptTOS,
		Cache:       autocert.DirCache(cacheDir),
		HostPolicy:  autocert.HostWhitelist(domains...),
		Email:       email,
	}

	// Create TLS config
	tlsConfig := m.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12

	return tlsConfig, nil
}

// configureTLS configures TLS for the server based on the provided configuration.
// Returns the TLS config and certificate/key file paths (if applicable).
func configureTLS(cfg Config) (*tls.Config, string, string, error) {
	// Option 1: Certificate files provided
	if cfg.SSLCert != "" && cfg.SSLKey != "" {
		// Validate that files exist
		if _, err := os.Stat(cfg.SSLCert); os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf("SSL certificate file not found: %s", cfg.SSLCert)
		}
		if _, err := os.Stat(cfg.SSLKey); os.IsNotExist(err) {
			return nil, "", "", fmt.Errorf("SSL key file not found: %s", cfg.SSLKey)
		}

		// Return basic TLS config - cert/key will be loaded by ListenAndServeTLS
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return tlsConfig, cfg.SSLCert, cfg.SSLKey, nil
	}

	// Option 2: Auto TLS (Let's Encrypt)
	if cfg.AutoTLS {
		tlsConfig, err := setupAutoTLS(cfg.AutoTLSDomains, cfg.AutoTLSEmail, cfg.AutoTLSCacheDir)
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to setup AutoTLS: %w", err)
		}
		return tlsConfig, "", "", nil
	}

	// Option 3: Self-signed certificate
	if cfg.SelfSignedSSL {
		host := cfg.Host
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}

		// Sanitize hostname for safe file naming
		safeHost := sanitizeHostname(host)

		// Lock to prevent concurrent certificate generation for the same host
		certGenerationMutex.Lock()
		defer certGenerationMutex.Unlock()

		// Get certificate directory
		certDir, err := getCertDirectory()
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to get certificate directory: %w", err)
		}

		// Check for existing valid certificates
		certFile := filepath.Join(certDir, fmt.Sprintf("%s-cert.pem", safeHost))
		keyFile := filepath.Join(certDir, fmt.Sprintf("%s-key.pem", safeHost))

		// If valid certificates exist, reuse them
		if isCertificateValid(certFile) {
			// Verify key file also exists
			if _, err := os.Stat(keyFile); err == nil {
				tlsConfig := &tls.Config{
					MinVersion: tls.VersionTLS12,
				}
				return tlsConfig, certFile, keyFile, nil
			}
		}

		// Generate new certificates
		certPEM, keyPEM, err := generateSelfSignedCert(host)
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to generate self-signed certificate: %w", err)
		}

		certFile, keyFile, err = saveCertToFiles(certPEM, keyPEM, host)
		if err != nil {
			return nil, "", "", fmt.Errorf("failed to save self-signed certificate: %w", err)
		}

		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		return tlsConfig, certFile, keyFile, nil
	}

	return nil, "", "", nil
}
