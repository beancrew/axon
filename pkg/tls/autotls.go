// Package autotls provides automatic TLS certificate generation for Axon.
// It generates a self-signed CA and a server certificate signed by that CA.
package autotls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// GenerateCA creates an ECDSA P-256 CA key and self-signed certificate valid
// for 10 years, writing ca.crt and ca.key under dir. Returns the paths to the
// generated files.
func GenerateCA(dir string) (caCertPath, caKeyPath string, err error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("autotls: create dir %q: %w", dir, err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("autotls: generate CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return "", "", err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "Axon CA",
			Organization: []string{"Axon"},
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("autotls: create CA cert: %w", err)
	}

	caCertPath = filepath.Join(dir, "ca.crt")
	if err := writePEM(caCertPath, "CERTIFICATE", certDER, 0644); err != nil {
		return "", "", err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("autotls: marshal CA key: %w", err)
	}
	caKeyPath = filepath.Join(dir, "ca.key")
	if err := writePEM(caKeyPath, "EC PRIVATE KEY", keyDER, 0600); err != nil {
		return "", "", err
	}

	return caCertPath, caKeyPath, nil
}

// GenerateServerCert creates an ECDSA P-256 server key and a certificate
// signed by the CA at caCertPath/caKeyPath, valid for 1 year. The SANs always
// include localhost and 127.0.0.1; any additional hosts are added as DNS names
// or IP addresses as appropriate. Writes server.crt and server.key under dir.
func GenerateServerCert(dir string, caCertPath, caKeyPath string, hosts ...string) (certPath, keyPath string, err error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", fmt.Errorf("autotls: create dir %q: %w", dir, err)
	}

	// Load CA cert and key.
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		return "", "", fmt.Errorf("autotls: read CA cert: %w", err)
	}
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		return "", "", fmt.Errorf("autotls: decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("autotls: parse CA cert: %w", err)
	}

	caKeyPEM, err := os.ReadFile(caKeyPath)
	if err != nil {
		return "", "", fmt.Errorf("autotls: read CA key: %w", err)
	}
	keyBlock, _ := pem.Decode(caKeyPEM)
	if keyBlock == nil {
		return "", "", fmt.Errorf("autotls: decode CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("autotls: parse CA key: %w", err)
	}

	// Generate server key.
	srvKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("autotls: generate server key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return "", "", err
	}

	dnsNames := []string{"localhost"}
	ipAddrs := []net.IP{net.ParseIP("127.0.0.1")}

	for _, h := range hosts {
		if h == "" || h == "localhost" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			ipAddrs = append(ipAddrs, ip)
		} else {
			dnsNames = append(dnsNames, h)
		}
	}

	cn := "localhost"
	if len(hosts) > 0 && hosts[0] != "" {
		cn = hosts[0]
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   cn,
			Organization: []string{"Axon"},
		},
		DNSNames:    dnsNames,
		IPAddresses: ipAddrs,
		NotBefore:   time.Now().Add(-time.Minute),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return "", "", fmt.Errorf("autotls: create server cert: %w", err)
	}

	certPath = filepath.Join(dir, "server.crt")
	if err := writePEM(certPath, "CERTIFICATE", certDER, 0644); err != nil {
		return "", "", err
	}

	keyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		return "", "", fmt.Errorf("autotls: marshal server key: %w", err)
	}
	keyPath = filepath.Join(dir, "server.key")
	if err := writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0600); err != nil {
		return "", "", err
	}

	return certPath, keyPath, nil
}

// EnsureTLS checks whether ca.crt already exists under dir. If it does, all
// paths are returned with generated=false. Otherwise a new CA and server cert
// are generated. hosts are passed to GenerateServerCert as SANs.
func EnsureTLS(dir string, hosts ...string) (caCertPath, serverCertPath, serverKeyPath string, generated bool, err error) {
	caCertPath = filepath.Join(dir, "ca.crt")
	var caKeyPath string
	serverCertPath = filepath.Join(dir, "server.crt")
	serverKeyPath = filepath.Join(dir, "server.key")

	if _, err := os.Stat(caCertPath); err == nil {
		caKeyPath = filepath.Join(dir, "ca.key")

		// Check if server cert exists; regenerate if missing.
		if _, serr := os.Stat(serverCertPath); serr != nil {
			serverCertPath, serverKeyPath, err = GenerateServerCert(dir, caCertPath, caKeyPath, hosts...)
			if err != nil {
				return "", "", "", false, fmt.Errorf("autotls: regenerate server cert: %w", err)
			}
			return caCertPath, serverCertPath, serverKeyPath, true, nil
		}

		// Check server cert expiry; renew if expiring within 30 days.
		if expiring, renewErr := certExpiringWithin(serverCertPath, 30*24*time.Hour); renewErr == nil && expiring {
			serverCertPath, serverKeyPath, err = GenerateServerCert(dir, caCertPath, caKeyPath, hosts...)
			if err != nil {
				return "", "", "", false, fmt.Errorf("autotls: renew server cert: %w", err)
			}
			return caCertPath, serverCertPath, serverKeyPath, true, nil
		}

		return caCertPath, serverCertPath, serverKeyPath, false, nil
	}

	caCertPath, caKeyPath, err = GenerateCA(dir)
	if err != nil {
		return "", "", "", false, err
	}

	serverCertPath, serverKeyPath, err = GenerateServerCert(dir, caCertPath, caKeyPath, hosts...)
	if err != nil {
		return "", "", "", false, err
	}

	return caCertPath, serverCertPath, serverKeyPath, true, nil
}

// CAFingerprint returns the SHA-256 fingerprint of the certificate at certPath
// formatted as colon-separated uppercase hex bytes (e.g. "AA:BB:CC:...").
func CAFingerprint(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("autotls: read cert %q: %w", certPath, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("autotls: decode PEM %q", certPath)
	}
	sum := sha256.Sum256(block.Bytes)
	b := make([]byte, 0, sha256.Size*3-1)
	for i, byt := range sum {
		if i > 0 {
			b = append(b, ':')
		}
		h := hex.EncodeToString([]byte{byt})
		b = append(b, []byte(h)...)
	}
	return string(b), nil
}

// certExpiringWithin checks if the PEM certificate at path expires within the
// given duration. Returns (true, nil) if it does, (false, nil) if it doesn't,
// or (false, err) if the cert cannot be read/parsed.
func certExpiringWithin(path string, within time.Duration) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return false, fmt.Errorf("autotls: decode PEM %q", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	return time.Until(cert.NotAfter) < within, nil
}

// randomSerial generates a random 128-bit serial number for a certificate.
func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("autotls: generate serial: %w", err)
	}
	return serial, nil
}

// writePEM encodes der as a PEM block of the given type and writes it to path
// with the specified file permissions.
func writePEM(path, pemType string, der []byte, mode os.FileMode) (retErr error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("autotls: open %q: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("autotls: close %q: %w", path, cerr)
		}
	}()
	if err := pem.Encode(f, &pem.Block{Type: pemType, Bytes: der}); err != nil {
		return fmt.Errorf("autotls: encode PEM %q: %w", path, err)
	}
	return nil
}
