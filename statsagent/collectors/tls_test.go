package collectors

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transportTLS(t *testing.T, c *http.Client) *tls.Config {
	t.Helper()
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.TLSClientConfig == nil {
		t.Fatalf("client has no TLS config: %#v", c.Transport)
	}
	return tr.TLSClientConfig
}

func writeKubeconfig(t *testing.T, server, caLine string) string {
	t.Helper()
	kc := "apiVersion: v1\nclusters:\n- name: c\n  cluster:\n    server: " + server + "\n" + caLine +
		"contexts:\n- name: x\n  context:\n    cluster: c\n    user: u\ncurrent-context: x\n" +
		"users:\n- name: u\n  user:\n    token: not-a-real-token\n"
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(kc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// No CA must mean the system roots, never skipped verification, whatever the
// host: a non-loopback API server and a loopback one alike.
func TestKubeconfigWithoutCANeverSkipsVerify(t *testing.T) {
	for _, server := range []string{
		"https://203.0.113.10:6443",
		"https://k8s.example.com:6443",
		"https://127.0.0.1:6443",
		"https://[::1]:6443",
		"https://localhost:6443",
	} {
		c, err := newK3sClient(writeKubeconfig(t, server, ""))
		if err != nil {
			t.Fatalf("%s: %v", server, err)
		}
		cfg := transportTLS(t, c.httpClient)
		if cfg.InsecureSkipVerify {
			t.Errorf("%s: InsecureSkipVerify must be false without a CA", server)
		}
		if cfg.RootCAs != nil {
			t.Errorf("%s: no CA given, want system roots (nil RootCAs)", server)
		}
		if cfg.MinVersion < tls.VersionTLS13 {
			t.Errorf("%s: MinVersion %x, want TLS 1.3", server, cfg.MinVersion)
		}
	}
}

// A self-signed server is refused when its CA is not supplied, loopback
// included, and accepted once the CA is in the pool.
func TestTLSClientFailsClosedOnUntrustedServer(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp, err := tlsClient(nil).Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("untrusted self-signed server must be refused")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("want a certificate error, got %v", err)
	}

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	resp, err = tlsClient(pool).Get(srv.URL)
	if err != nil {
		t.Fatalf("server signed by the supplied CA must be accepted: %v", err)
	}
	resp.Body.Close()
}

func TestLoadCARejectsFileWithoutCertificates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(p, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pool, err := loadCA(p); err == nil || pool != nil {
		t.Fatalf("a CA file without certificates must be an error, got pool=%v err=%v", pool, err)
	}
	if _, err := loadCA(filepath.Join(t.TempDir(), "missing.crt")); err == nil {
		t.Fatal("a missing CA file must be an error")
	}
}

// A kubeconfig naming a CA that cannot be loaded must not fall back to an
// unverified or system-roots client.
func TestKubeconfigWithBadCAFails(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "empty.crt")
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newK3sClient(writeKubeconfig(t, "https://203.0.113.10:6443", "    certificate-authority: "+bad+"\n"))
	if err == nil {
		t.Fatal("unusable kubeconfig CA must fail client creation")
	}
}

func TestKubeconfigWithCAUsesIt(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	ca := filepath.Join(t.TempDir(), "ca.crt")
	b := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(ca, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := newK3sClient(writeKubeconfig(t, srv.URL, "    certificate-authority: "+ca+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := transportTLS(t, c.httpClient)
	if cfg.RootCAs == nil || cfg.InsecureSkipVerify {
		t.Fatalf("kubeconfig CA must be the trust root: %+v", cfg)
	}
	resp, err := c.httpClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("server signed by kubeconfig CA must be accepted: %v", err)
	}
	resp.Body.Close()
}
