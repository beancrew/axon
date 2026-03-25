package autotls_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"testing"

	autotls "github.com/garysng/axon/pkg/tls"
)

// TestEnsureTLS_GeneratesCerts verifies that EnsureTLS creates all four files
// and produces a valid CA-signed server certificate.
func TestEnsureTLS_GeneratesCerts(t *testing.T) {
	dir := t.TempDir()

	paths, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}

	// All four files must exist.
	for _, f := range []string{paths.CACert, paths.CAKey, paths.ServerCert, paths.ServerKey} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %q to exist: %v", f, err)
		}
	}

	// Server cert + key must load as a valid TLS key pair.
	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("empty certificate chain")
	}

	// Parse the server cert and verify it against the CA.
	parsedCert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	caCertPEM, err := os.ReadFile(paths.CACert)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("failed to append CA cert to pool")
	}

	opts := x509.VerifyOptions{
		DNSName: "localhost",
		Roots:   pool,
	}
	if _, err := parsedCert.Verify(opts); err != nil {
		t.Errorf("server cert verification failed: %v", err)
	}
}

// TestEnsureTLS_Idempotent verifies that a second call does not regenerate
// the certificates (files are left untouched).
func TestEnsureTLS_Idempotent(t *testing.T) {
	dir := t.TempDir()

	p1, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("first EnsureTLS: %v", err)
	}

	info1, err := os.Stat(p1.CACert)
	if err != nil {
		t.Fatal(err)
	}

	p2, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("second EnsureTLS: %v", err)
	}

	info2, err := os.Stat(p2.CACert)
	if err != nil {
		t.Fatal(err)
	}

	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("EnsureTLS regenerated certs on second call (should be idempotent)")
	}
}

// TestEnsureTLS_CustomHostname verifies that the server cert includes the
// provided hostname as a SAN DNS name.
func TestEnsureTLS_CustomHostname(t *testing.T) {
	dir := t.TempDir()
	const host = "my-server.example.com"

	paths, err := autotls.EnsureTLS(dir, host)
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}

	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	for _, name := range parsed.DNSNames {
		if name == host {
			return
		}
	}
	t.Errorf("expected SAN DNS name %q; got %v", host, parsed.DNSNames)
}

// TestEnsureTLS_IPHostname verifies that an IP-format hostname is added as a
// SAN IP address rather than a DNS name.
func TestEnsureTLS_IPHostname(t *testing.T) {
	dir := t.TempDir()
	const host = "192.168.1.10"

	paths, err := autotls.EnsureTLS(dir, host)
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}

	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	want := net.ParseIP(host)
	for _, ip := range parsed.IPAddresses {
		if ip.Equal(want) {
			return
		}
	}
	t.Errorf("expected SAN IP %q; got %v", host, parsed.IPAddresses)
}

// TestEnsureTLS_LocalhostSAN verifies that localhost and 127.0.0.1 are always
// present as SANs regardless of hostname.
func TestEnsureTLS_LocalhostSAN(t *testing.T) {
	dir := t.TempDir()

	paths, err := autotls.EnsureTLS(dir, "axon-server.internal")
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}

	cert, err := tls.LoadX509KeyPair(paths.ServerCert, paths.ServerKey)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	foundLocalhost := false
	for _, name := range parsed.DNSNames {
		if name == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Errorf("expected 'localhost' in DNS SANs; got %v", parsed.DNSNames)
	}

	want127 := net.ParseIP("127.0.0.1")
	found127 := false
	for _, ip := range parsed.IPAddresses {
		if ip.Equal(want127) {
			found127 = true
		}
	}
	if !found127 {
		t.Errorf("expected '127.0.0.1' in IP SANs; got %v", parsed.IPAddresses)
	}
}
