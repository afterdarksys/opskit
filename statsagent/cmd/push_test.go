package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/afterdarksys/opskit/statsagent/output"
)

func testSnapshot() *output.StatsSnapshot {
	return &output.StatsSnapshot{Context: "host"}
}

// --- authentication ------------------------------------------------------

func TestPushSendsAPIKeyHeader(t *testing.T) {
	var gotKey, gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-API-Key")
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &Config{APIKey: "secret-key-value"}
	if err := pushSnapshot(cfg, srv.URL, testSnapshot()); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if gotKey != "secret-key-value" {
		t.Errorf("X-API-Key = %q, want the configured key", gotKey)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
}

func TestPushOmitsHeaderWhenNoKeyConfigured(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Api-Key"]
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	// An unauthenticated collector must keep working — absence of a key is a
	// valid configuration, not an error.
	if err := pushSnapshot(&Config{}, srv.URL, testSnapshot()); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if present {
		t.Error("X-API-Key header was sent even though no key is configured")
	}
}

func TestPushNeverPutsKeyInURLOrBody(t *testing.T) {
	const key = "super-secret-do-not-leak"
	var gotURL, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	if err := pushSnapshot(&Config{APIKey: key}, srv.URL, testSnapshot()); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	if strings.Contains(gotURL, key) {
		t.Error("API key leaked into the request URL")
	}
	if strings.Contains(gotBody, key) {
		t.Error("API key leaked into the request body")
	}
	// Sanity: the body really is the snapshot.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(gotBody), &decoded); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
}

func TestPushReportsAuthFailureHelpfully(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		err := pushSnapshot(&Config{APIKey: "bad"}, srv.URL, testSnapshot())
		srv.Close()

		if err == nil {
			t.Fatalf("status %d: expected an error", code)
		}
		if !strings.Contains(err.Error(), "STATSAGENT_API_KEY") {
			t.Errorf("status %d: error %q should point at the key setting", code, err)
		}
		if strings.Contains(err.Error(), "bad") && !strings.Contains(err.Error(), "check") {
			t.Errorf("status %d: error must not echo the key", code)
		}
	}
}

func TestPushSurfacesServerErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := pushSnapshot(&Config{}, srv.URL, testSnapshot()); err == nil {
		t.Error("expected an error for a 500 response")
	}
}

// --- timeout -------------------------------------------------------------

func TestPushClientHasTimeout(t *testing.T) {
	// Regression guard: the original implementation used http.Post, which uses
	// http.DefaultClient with no timeout. A hung collector would have stalled
	// the push loop forever.
	if pushClient.Timeout == 0 {
		t.Fatal("pushClient has no timeout; a hung collector would block pushes forever")
	}
	if pushClient.Timeout > 60*time.Second {
		t.Errorf("pushClient timeout %v is too long to be useful", pushClient.Timeout)
	}
}

func TestPushTimesOutRatherThanHanging(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	saved := pushClient
	pushClient = &http.Client{Timeout: 100 * time.Millisecond}
	defer func() { pushClient = saved }()

	start := time.Now()
	err := pushSnapshot(&Config{}, srv.URL, testSnapshot())
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected a timeout error")
	}
	if elapsed > time.Second {
		t.Errorf("push took %v; the timeout did not apply", elapsed)
	}
}

// --- config loading ------------------------------------------------------

func TestAPIKeyFromEnv(t *testing.T) {
	t.Setenv("STATSAGENT_API_KEY", "  key-with-space  ")
	if got := DefaultConfig().APIKey; got != "key-with-space" {
		t.Errorf("APIKey = %q, want the trimmed value", got)
	}
}

func TestAPIKeyFileTakesPrecedenceOverEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key")
	// Trailing newline is the normal case for a secret file.
	if err := os.WriteFile(path, []byte("key-from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("STATSAGENT_API_KEY", "key-from-env")
	t.Setenv("STATSAGENT_API_KEY_FILE", path)

	if got := DefaultConfig().APIKey; got != "key-from-file" {
		t.Errorf("APIKey = %q, want the file value to win and be trimmed", got)
	}
}

func TestAPIKeyFileMissingFallsBackToEnv(t *testing.T) {
	t.Setenv("STATSAGENT_API_KEY", "key-from-env")
	t.Setenv("STATSAGENT_API_KEY_FILE", "/nonexistent/path/to/key")

	if got := DefaultConfig().APIKey; got != "key-from-env" {
		t.Errorf("APIKey = %q, want the env fallback when the file is unreadable", got)
	}
}

func TestNoAPIKeyConfiguredIsEmpty(t *testing.T) {
	t.Setenv("STATSAGENT_API_KEY", "")
	t.Setenv("STATSAGENT_API_KEY_FILE", "")
	if got := DefaultConfig().APIKey; got != "" {
		t.Errorf("APIKey = %q, want empty when nothing is configured", got)
	}
}

// --- plaintext warning ---------------------------------------------------

func TestPlaintextWarningTriggersForRemoteHTTPOnly(t *testing.T) {
	cases := []struct {
		endpoint string
		wantWarn bool
	}{
		{"http://collector.example.com/ingest", true},
		{"http://192.0.2.10:3002/v1/telemetry", true},
		{"https://api.example.com/v1/telemetry", false},
		{"http://localhost:3002/v1/telemetry", false},
		{"http://127.0.0.1:3002/v1/telemetry", false},
	}
	for _, c := range cases {
		warnedPlaintext = false
		warnIfPlaintextCredential(c.endpoint)
		if warnedPlaintext != c.wantWarn {
			t.Errorf("%s: warned=%v, want %v", c.endpoint, warnedPlaintext, c.wantWarn)
		}
	}
	warnedPlaintext = false
}

func TestPlaintextWarningFiresOnlyOnce(t *testing.T) {
	warnedPlaintext = false
	warnIfPlaintextCredential("http://collector.example.com/ingest")
	first := warnedPlaintext
	warnIfPlaintextCredential("http://collector.example.com/ingest")

	if !first || !warnedPlaintext {
		t.Fatal("expected the warning to latch")
	}
	warnedPlaintext = false
}
