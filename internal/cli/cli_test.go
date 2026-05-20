package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRecordsAddDryRunDoesNotRequireSession(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")

	code := Main([]string{
		"--config", configPath,
		"records", "add",
		"--domain", "example.com",
		"--type", "A",
		"--name", "www",
		"--data", "192.0.2.1",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Main() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Dry run only") {
		t.Fatalf("stdout = %q, want dry-run message", stdout.String())
	}
}

func TestRecordsDeleteRequiresData(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")

	code := Main([]string{
		"--config", configPath,
		"records", "delete",
		"--domain", "example.com",
		"--type", "TXT",
		"--name", "_acme-challenge",
	}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("Main() code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "delete requires --data") {
		t.Fatalf("stderr = %q, want --data error", stderr.String())
	}
}

func TestAuthLoginWritesSession(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")

	code := Main([]string{
		"--config", configPath,
		"auth", "login",
		"--api-key", "test-api-key",
		"--api-secret", "test-api-secret",
		"--env", "ote",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Main() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Saved GoDaddy session") {
		t.Fatalf("stdout = %q, want saved message", stdout.String())
	}
}

func TestRecordsAddApplyCallsGoDaddyAPI(t *testing.T) {
	var called atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)

		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/v1/domains/example.com/records" {
			t.Errorf("path = %s, want /v1/domains/example.com/records", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "sso-key test-api-key:test-api-secret" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}

		var body []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) != 1 {
			t.Errorf("body length = %d, want 1", len(body))
		} else {
			if body[0]["type"] != "A" || body[0]["name"] != "www" || body[0]["data"] != "192.0.2.1" {
				t.Errorf("body[0] = %#v", body[0])
			}
			if body[0]["ttl"] != float64(600) {
				t.Errorf("ttl = %#v, want 600", body[0]["ttl"])
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")
	code := Main([]string{
		"--config", configPath,
		"auth", "login",
		"--api-key", "test-api-key",
		"--api-secret", "test-api-secret",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("auth login code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{
		"--config", configPath,
		"--base-url", server.URL,
		"records", "add",
		"--domain", "example.com",
		"--type", "A",
		"--name", "www",
		"--data", "192.0.2.1",
		"--ttl", "600",
		"--apply",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("records add code = %d, stderr = %q", code, stderr.String())
	}
	if !called.Load() {
		t.Fatal("mock GoDaddy API was not called")
	}
	if !strings.Contains(stdout.String(), "Added DNS record") {
		t.Fatalf("stdout = %q, want added message", stdout.String())
	}
}
