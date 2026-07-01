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

func TestAuthDelegatePrintsAccessURL(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")

	code := Main([]string{
		"--config", configPath,
		"auth", "delegate",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Main() code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "https://account.godaddy.com/access") {
		t.Fatalf("stdout = %q, want delegate access URL", stdout.String())
	}
	if !strings.Contains(stdout.String(), "--shopper-id") {
		t.Fatalf("stdout = %q, want --shopper-id guidance", stdout.String())
	}
}

func TestAuthDelegateJSONReportsConfiguredShopper(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")

	code := Main([]string{
		"--config", configPath,
		"--shopper-id", "123456789",
		"--json",
		"auth", "delegate",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("Main() code = %d, stderr = %q", code, stderr.String())
	}

	var payload struct {
		DelegateAccessURL string `json:"delegate_access_url"`
		ShopperID         string `json:"shopper_id"`
		ShopperIDSource   string `json:"shopper_id_source"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v, stdout = %q", err, stdout.String())
	}
	if payload.DelegateAccessURL != "https://account.godaddy.com/access" {
		t.Fatalf("delegate_access_url = %q", payload.DelegateAccessURL)
	}
	if payload.ShopperID != "123456789" || payload.ShopperIDSource != "flag" {
		t.Fatalf("shopper_id = %q (source %q), want 123456789 from flag", payload.ShopperID, payload.ShopperIDSource)
	}
}

func TestVersionFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Main([]string{"--version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "gogodaddy ") {
		t.Fatalf("stdout = %q, want version line", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Main([]string{"--json", "-V"}, &stdout, &stderr); code != 0 {
		t.Fatalf("json code = %d, stderr = %q", code, stderr.String())
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v, stdout = %q", err, stdout.String())
	}
	if payload.Version == "" {
		t.Fatalf("version empty, stdout = %q", stdout.String())
	}
}

func TestRecordsListJSONEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/domains/example.com/records/A/www" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"type":"A","name":"www","data":"192.0.2.1","ttl":600}]`))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")
	if code := Main([]string{"--config", configPath, "auth", "login", "--api-key", "k", "--api-secret", "s"}, &stdout, &stderr); code != 0 {
		t.Fatalf("auth login code = %d, stderr = %q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code := Main([]string{
		"--config", configPath, "--base-url", server.URL, "--json",
		"records", "list", "--domain", "example.com", "--type", "a", "--name", "www",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("records list code = %d, stderr = %q", code, stderr.String())
	}

	var env struct {
		Domain  string `json:"domain"`
		Type    string `json:"type"`
		Name    string `json:"name"`
		Count   int    `json:"count"`
		Records []struct {
			Data string `json:"data"`
		} `json:"records"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v, stdout = %q", err, stdout.String())
	}
	if env.Domain != "example.com" || env.Type != "A" || env.Name != "www" {
		t.Fatalf("envelope meta = %+v", env)
	}
	if env.Count != 1 || len(env.Records) != 1 || env.Records[0].Data != "192.0.2.1" {
		t.Fatalf("envelope records = %+v", env)
	}
}

func TestRecordsAddDryRunEmitsNextCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")
	code := Main([]string{
		"--config", configPath, "--json",
		"records", "add", "--domain", "example.com", "--type", "A", "--name", "www", "--data", "192.0.2.1", "--ttl", "600",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("dry-run code = %d, stderr = %q", code, stderr.String())
	}

	var payload struct {
		DryRun      bool   `json:"dry_run"`
		NextCommand string `json:"next_command"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v, stdout = %q", err, stdout.String())
	}
	if !payload.DryRun {
		t.Fatalf("dry_run = false, want true")
	}
	for _, want := range []string{"records add", "--domain example.com", "--ttl 600", "--apply"} {
		if !strings.Contains(payload.NextCommand, want) {
			t.Fatalf("next_command = %q, missing %q", payload.NextCommand, want)
		}
	}
}

func TestRecordsErrorJSONIsStructured(t *testing.T) {
	var stdout, stderr bytes.Buffer
	configPath := filepath.Join(t.TempDir(), "session.json")
	code := Main([]string{
		"--config", configPath, "--json",
		"records", "add", "--type", "A", "--name", "www", "--data", "192.0.2.1",
	}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}

	var e struct {
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &e); err != nil {
		t.Fatalf("decode error envelope: %v, stderr = %q", err, stderr.String())
	}
	if e.Error != "missing_flag" || e.ExitCode != 2 {
		t.Fatalf("error envelope = %+v", e)
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
