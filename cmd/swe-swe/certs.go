package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// generateSelfSignedCert creates a self-signed TLS certificate and key for HTTPS.
// The certificate is valid for localhost, 127.0.0.1, and optionally an extra host.
// Files are written to certsDir as server.crt and server.key.
func generateSelfSignedCert(certsDir string, extraHost string) error {
	// Generate RSA private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	// Base DNS names and IPs
	dnsNames := []string{"localhost", "*.localhost"}
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	// Add extra host if provided (can be IP or hostname)
	commonName := "localhost"
	if extraHost != "" {
		if ip := net.ParseIP(extraHost); ip != nil {
			// It's an IP address
			ipAddresses = append(ipAddresses, ip)
		} else {
			// It's a hostname
			dnsNames = append(dnsNames, extraHost)
		}
		commonName = extraHost
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"swe-swe"},
			CommonName:   commonName,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // Required for iOS to show trust toggle in Certificate Trust Settings
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write certificate to file
	certPath := filepath.Join(certsDir, "server.crt")
	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create certificate file: %w", err)
	}
	defer certFile.Close()

	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to write certificate: %w", err)
	}

	// Write private key to file
	keyPath := filepath.Join(certsDir, "server.key")
	keyFile, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %w", err)
	}
	defer keyFile.Close()

	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}

	if err := pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}

	return nil
}

// handleCertificatesAndEnv detects and copies enterprise certificates for Docker builds,
// and writes the .env file with PROJECT_NAME and certificate configuration.
// Supports NODE_EXTRA_CA_CERTS, SSL_CERT_FILE, and NODE_EXTRA_CA_CERTS_BUNDLE environment variables
// for users behind corporate firewalls or VPNs (Cloudflare Warp, etc)
// Returns true if any certificates were found and copied, false otherwise
func handleCertificatesAndEnv(sweDir, certsDir, projectName string) bool {
	// Check for certificate environment variables
	certEnvVars := []string{
		"NODE_EXTRA_CA_CERTS",
		"SSL_CERT_FILE",
		"NODE_EXTRA_CA_CERTS_BUNDLE",
	}

	var certPaths []string
	// Always start with PROJECT_NAME
	envFileContent := fmt.Sprintf("PROJECT_NAME=%s\n", projectName)

	for _, envVar := range certEnvVars {
		certPath := os.Getenv(envVar)
		if certPath == "" {
			continue
		}

		// Verify certificate file exists
		_, err := os.Stat(certPath)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Printf("Warning: %s points to non-existent file: %s (ignoring)\n", envVar, certPath)
			} else {
				fmt.Printf("Warning: Could not access %s=%s: %v (ignoring)\n", envVar, certPath, err)
			}
			continue
		}

		// Copy certificate file to .swe-swe/certs/
		certFilename := filepath.Base(certPath)
		destCertPath := filepath.Join(certsDir, certFilename)

		if err := copyFile(certPath, destCertPath); err != nil {
			fmt.Printf("Warning: Failed to copy certificate %s: %v (ignoring)\n", certPath, err)
			continue
		}

		fmt.Printf("Copied enterprise certificate: %s → %s\n", certPath, destCertPath)

		// Track certificate for .env file
		certPaths = append(certPaths, certFilename)
		envFileContent += fmt.Sprintf("%s=/swe-swe/certs/%s\n", envVar, certFilename)
	}

	// Always create .env file (at minimum contains PROJECT_NAME)
	envFilePath := filepath.Join(sweDir, ".env")
	if err := os.WriteFile(envFilePath, []byte(envFileContent), 0644); err != nil {
		fmt.Printf("Warning: Failed to create .env file: %v\n", err)
		return false
	}
	if len(certPaths) > 0 {
		fmt.Printf("Created %s with PROJECT_NAME and certificate configuration\n", envFilePath)
	}

	return len(certPaths) > 0
}
