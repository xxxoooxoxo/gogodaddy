package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveLoadSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "github.com/xxxoooxoxo/gogodaddy", "session.json")

	in := Session{
		APIKey:      "test-api-key",
		APISecret:   "test-api-secret",
		Environment: OTEEnv,
		ShopperID:   "shopper",
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if out.APIKey != in.APIKey || out.APISecret != in.APISecret {
		t.Fatalf("loaded credentials mismatch: %#v", out)
	}
	if out.BaseURL != OTEBaseURL {
		t.Fatalf("BaseURL = %q, want %q", out.BaseURL, OTEBaseURL)
	}
	if out.CreatedAt.IsZero() || out.UpdatedAt.IsZero() {
		t.Fatalf("timestamps were not set: %#v", out)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat session: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %o, want 600", got)
		}
	}
}

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		override    string
		want        string
		wantErr     bool
	}{
		{name: "production default", want: ProductionBaseURL},
		{name: "production explicit", environment: ProductionEnv, want: ProductionBaseURL},
		{name: "ote", environment: OTEEnv, want: OTEBaseURL},
		{name: "override", environment: OTEEnv, override: "https://example.test", want: "https://example.test"},
		{name: "invalid", environment: "staging", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveBaseURL(tt.environment, tt.override)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ResolveBaseURL() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveBaseURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ResolveBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}
