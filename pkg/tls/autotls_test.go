package autotls_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"strings"
	"testing"

	autotls "github.com/garysng/axon/pkg/tls"
)

// TestGenerateCA verifies that GenerateCA creates valid PEM files containing
// a self-signed CA certificate with IsCA=true.
func TestGenerateCA(t *testing.T) {
	dir := t.TempDir()

	caCertPath, caKeyPath, err := autotls.GenerateCA(dir)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	for _, f := range []string{caCertPath, caKeyPath} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %q to exist: %v", f, err)
		}
	}

	// Load and parse the CA cert.
	certPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("AppendCertsFromPEM failed")
	}

	pair, err := tls.LoadX509KeyPair(caCertPath, caKeyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair (CA): %v", err)
	}
	parsed, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if !parsed.IsCA {
		t.Error("expected IsCA=true")
	}
}

// TestGenerateServerCert verifies that GenerateServerCert produces a server
// certificate signed by the CA and containing the expected SANs.
func TestGenerateServerCert(t *testing.T) {
	dir := t.TempDir()

	caCertPath, caKeyPath, err := autotls.GenerateCA(dir)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	const host = "test-server.example.com"
	certPath, keyPath, err := autotls.GenerateServerCert(dir, caCertPath, caKeyPath, host)
	if err != nil {
		t.Fatalf("GenerateServerCert: %v", err)
	}

	for _, f := range []string{certPath, keyPath} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %q to exist: %v", f, err)
		}
	}

	// Load key pair.
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	parsed, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}

	// Verify against CA.
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	if _, err := parsed.Verify(x509.VerifyOptions{DNSName: host, Roots: pool}); err != nil {
		t.Errorf("server cert verification failed: %v", err)
	}

	// Must include localhost and 127.0.0.1.
	foundLocalhost := false
	for _, n := range parsed.DNSNames {
		if n == "localhost" {
			foundLocalhost = true
		}
	}
	if !foundLocalhost {
		t.Errorf("missing localhost in DNSNames: %v", parsed.DNSNames)
	}

	want127 := net.ParseIP("127.0.0.1")
	found127 := false
	for _, ip := range parsed.IPAddresses {
		if ip.Equal(want127) {
			found127 = true
		}
	}
	if !found127 {
		t.Errorf("missing 127.0.0.1 in IPAddresses: %v", parsed.IPAddresses)
	}
}

// TestEnsureTLS_Generates verifies that EnsureTLS creates certs on first call
// and returns generated=true.
func TestEnsureTLS_Generates(t *testing.T) {
	dir := t.TempDir()

	caCertPath, srvCertPath, srvKeyPath, generated, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}
	if !generated {
		t.Error("expected generated=true on first call")
	}

	for _, f := range []string{caCertPath, srvCertPath, srvKeyPath} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected file %q to exist: %v", f, err)
		}
	}
}

// TestEnsureTLS_Idempotent verifies that a second call skips generation.
func TestEnsureTLS_Idempotent(t *testing.T) {
	dir := t.TempDir()

	_, _, _, _, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("first EnsureTLS: %v", err)
	}

	caCertPath, _, _, generated, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("second EnsureTLS: %v", err)
	}
	if generated {
		t.Error("expected generated=false on second call")
	}

	if _, err := os.Stat(caCertPath); err != nil {
		t.Errorf("CA cert missing after second call: %v", err)
	}
}

// TestCAFingerprint verifies that CAFingerprint returns a colon-separated
// uppercase hex string with 64 hex chars (32 bytes × 2 + 31 colons = 95 chars).
func TestCAFingerprint(t *testing.T) {
	dir := t.TempDir()

	caCertPath, _, err := autotls.GenerateCA(dir)
	if err != nil {
		t.Fatalf("GenerateCA: %v", err)
	}

	fp, err := autotls.CAFingerprint(caCertPath)
	if err != nil {
		t.Fatalf("CAFingerprint: %v", err)
	}

	parts := strings.Split(fp, ":")
	if len(parts) != 32 {
		t.Errorf("expected 32 colon-separated parts, got %d: %q", len(parts), fp)
	}
	for _, p := range parts {
		if len(p) != 2 {
			t.Errorf("expected 2-char hex byte, got %q in %q", p, fp)
		}
	}

	// Calling again must return the same value.
	fp2, err := autotls.CAFingerprint(caCertPath)
	if err != nil {
		t.Fatalf("CAFingerprint second call: %v", err)
	}
	if fp != fp2 {
		t.Errorf("fingerprint not stable: %q vs %q", fp, fp2)
	}
}

// TestTLSHandshake verifies that the generated certs can complete a real TLS
// handshake between an in-process client and server.
func TestTLSHandshake(t *testing.T) {
	dir := t.TempDir()

	caCertPath, srvCertPath, srvKeyPath, _, err := autotls.EnsureTLS(dir, "localhost")
	if err != nil {
		t.Fatalf("EnsureTLS: %v", err)
	}

	// Build server TLS config.
	srvCert, err := tls.LoadX509KeyPair(srvCertPath, srvKeyPath)
	if err != nil {
		t.Fatalf("LoadX509KeyPair: %v", err)
	}
	srvTLS := &tls.Config{Certificates: []tls.Certificate{srvCert}}

	ln, err := tls.Listen("tcp", "127.0.0.1:0", srvTLS)
	if err != nil {
		t.Fatalf("tls.Listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		// Force handshake.
		err = conn.(*tls.Conn).Handshake()
		_ = conn.Close()
		errCh <- err
	}()

	// Build client TLS config with the generated CA.
	caCertPEM, err := os.ReadFile(caCertPath)
	if err != nil {
		t.Fatalf("read CA cert: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("AppendCertsFromPEM failed")
	}
	clientTLS := &tls.Config{RootCAs: pool, ServerName: "localhost"}

	conn, err := tls.Dial("tcp", ln.Addr().String(), clientTLS)
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	_ = conn.Close()

	if err := <-errCh; err != nil {
		t.Errorf("server handshake error: %v", err)
	}
}
