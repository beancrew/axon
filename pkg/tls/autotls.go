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
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Paths holds the file paths for the generated TLS certificates and keys.
type Paths struct {
	CACert     string // CA certificate (PEM)
	CAKey      string // CA private key (PEM)
	ServerCert string // Server certificate signed by CA (PEM)
	ServerKey  string // Server private key (PEM)
}

// dirPaths returns the standard file paths for certs under the given directory.
func dirPaths(dir string) Paths {
	return Paths{
		CACert:     filepath.Join(dir, "ca.crt"),
		CAKey:      filepath.Join(dir, "ca.key"),
		ServerCert: filepath.Join(dir, "server.crt"),
		ServerKey:  filepath.Join(dir, "server.key"),
	}
}

// EnsureTLS checks whether all certificate files exist under dir. If any are
// missing, a new self-signed CA and server certificate are generated and
// written to dir. The CA fingerprint (SHA-256) is logged on generation.
// Returns the file paths for use with tls.LoadX509KeyPair and friends.
func EnsureTLS(dir, hostname string) (Paths, error) {
	p := dirPaths(dir)

	// If all four files are already present, nothing to do.
	allExist := true
	for _, f := range []string{p.CACert, p.CAKey, p.ServerCert, p.ServerKey} {
		if _, err := os.Stat(f); err != nil {
			allExist = false
			break
		}
	}
	if allExist {
		return p, nil
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return Paths{}, fmt.Errorf("autotls: create dir %q: %w", dir, err)
	}

	caKey, caCert, err := generateCA(p)
	if err != nil {
		return Paths{}, err
	}

	// Log the CA fingerprint so operators can pin it on clients.
	fp := sha256.Sum256(caCert.Raw)
	log.Printf("autotls: generated CA certificate; SHA-256 fingerprint: %X", fp)

	if err := generateServerCert(p, hostname, caKey, caCert); err != nil {
		return Paths{}, err
	}

	return p, nil
}

// generateCA creates an ECDSA P-256 CA key and self-signed certificate valid
// for 10 years, writes them to p.CAKey and p.CACert, and returns the parsed
// key and certificate for use when signing the server cert.
func generateCA(p Paths) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("autotls: generate CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("autotls: create CA cert: %w", err)
	}

	if err := writePEM(p.CACert, "CERTIFICATE", certDER, 0644); err != nil {
		return nil, nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("autotls: marshal CA key: %w", err)
	}
	if err := writePEM(p.CAKey, "EC PRIVATE KEY", keyDER, 0600); err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, fmt.Errorf("autotls: parse CA cert: %w", err)
	}
	return key, cert, nil
}

// generateServerCert creates an ECDSA P-256 server key and a certificate
// signed by the given CA, valid for 1 year. The SANs include localhost,
// 127.0.0.1, and the provided hostname (added as DNS name or IP as appropriate).
func generateServerCert(p Paths, hostname string, caKey *ecdsa.PrivateKey, caCert *x509.Certificate) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("autotls: generate server key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return err
	}

	dnsNames := []string{"localhost"}
	ipAddrs := []net.IP{net.ParseIP("127.0.0.1")}

	if hostname != "" && hostname != "localhost" {
		if ip := net.ParseIP(hostname); ip != nil {
			ipAddrs = append(ipAddrs, ip)
		} else {
			dnsNames = append(dnsNames, hostname)
		}
	}

	cn := hostname
	if cn == "" {
		cn = "localhost"
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

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("autotls: create server cert: %w", err)
	}

	if err := writePEM(p.ServerCert, "CERTIFICATE", certDER, 0644); err != nil {
		return err
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("autotls: marshal server key: %w", err)
	}
	return writePEM(p.ServerKey, "EC PRIVATE KEY", keyDER, 0600)
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
