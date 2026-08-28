// The tests in this file bind a real TCP socket rather than following
// docs/TESTS.md's httptest.NewRecorder()/s.Handler().ServeHTTP(...)
// convention, because that convention has no way to exercise which of
// ListenAndServe/ListenAndServeTLS actually gets called — the branch under
// test here is the network-level choice itself, not request routing or
// middleware. Do not take this file as the template for a handler test;
// use handlers_test.go's newTestServer + Handler().ServeHTTP for that.
package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateSelfSignedCert writes a throwaway self-signed certificate/key pair
// to dir and returns their paths, so tests can exercise ListenAndServeTLS
// without a real CA-issued certificate.
func generateSelfSignedCert(t *testing.T, dir string) (certFile, keyFile string) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
}

// freeAddr reserves an ephemeral local port and immediately releases it, so
// the caller can hand a concrete "host:port" address (rather than ":0") to
// http.Server, whose Addr field ListenAndServe/ListenAndServeTLS read as-is.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// waitUntilUp polls dial until it succeeds or the deadline passes. errCh is
// the server's ListenAndServe/ListenAndServeTLS result: if the server
// already exited (e.g. an unreadable cert pair, or freeAddr's reserved port
// getting taken by another process before the server could bind it), that
// is the actual failure and is reported immediately instead of masking it
// behind a generic "did not come up in time" timeout.
func waitUntilUp(t *testing.T, errCh <-chan error, dial func() error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			t.Fatalf("server stopped before it came up: %v", err)
		default:
		}
		if err := dial(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not come up in time: %v", lastErr)
}

func TestListenAndServe_TLSConfigured(t *testing.T) {
	certFile, keyFile := generateSelfSignedCert(t, t.TempDir())
	addr := freeAddr(t)
	s, _ := newTestServer(Config{ListenAddr: addr, TLSCertFile: certFile, TLSKeyFile: keyFile})

	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()

	waitUntilUp(t, errCh, func() error {
		conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return err
		}
		_ = conn.Close()
		return nil
	})

	// A plain (non-TLS) request to the same address must not be served as a
	// normal 200 response, confirming the listener is actually speaking TLS
	// rather than plain HTTP. net/http's TLS listener responds to a bare
	// HTTP request with a 400 explaining the mismatch rather than closing
	// the connection outright, so check for that rather than a dial error.
	plainClient := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := plainClient.Get("http://" + addr + "/healthz")
	if err == nil {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("plain HTTP request to a TLS-configured server got 200 OK, want a TLS/connection error or a non-200 status")
		}
	}
}

func TestListenAndServe_PlainHTTPByDefault(t *testing.T) {
	addr := freeAddr(t)
	s, _ := newTestServer(Config{ListenAddr: addr})

	errCh := make(chan error, 1)
	go func() { errCh <- s.ListenAndServe() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	}()

	client := http.Client{Timeout: 500 * time.Millisecond}
	waitUntilUp(t, errCh, func() error {
		resp, err := client.Get("http://" + addr + "/healthz")
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	})
}
