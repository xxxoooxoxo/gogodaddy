package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	AppDirName        = "gogodaddy"
	SessionFileName   = "session.json"
	ProductionEnv     = "production"
	OTEEnv            = "ote"
	ProductionBaseURL = "https://api.godaddy.com"
	OTEBaseURL        = "https://api.ote-godaddy.com"
)

var ErrNoSession = errors.New("no saved GoDaddy session")

type Session struct {
	APIKey      string    `json:"api_key"`
	APISecret   string    `json:"api_secret"`
	Environment string    `json:"environment"`
	BaseURL     string    `json:"base_url"`
	ShopperID   string    `json:"shopper_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func DefaultPath() (string, error) {
	if path := os.Getenv("GODADDY_CLI_CONFIG"); path != "" {
		return path, nil
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, AppDirName, SessionFileName), nil
}

func ResolveBaseURL(environment, override string) (string, error) {
	if override != "" {
		return override, nil
	}

	switch environment {
	case "", ProductionEnv:
		return ProductionBaseURL, nil
	case OTEEnv:
		return OTEBaseURL, nil
	default:
		return "", fmt.Errorf("unsupported environment %q, expected %q or %q", environment, ProductionEnv, OTEEnv)
	}
}

func Load(path string) (Session, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Session{}, ErrNoSession
		}
		return Session{}, err
	}

	var session Session
	if err := json.Unmarshal(b, &session); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	if session.APIKey == "" || session.APISecret == "" {
		return Session{}, errors.New("saved session is missing api_key or api_secret")
	}
	if session.BaseURL == "" {
		baseURL, err := ResolveBaseURL(session.Environment, "")
		if err != nil {
			return Session{}, err
		}
		session.BaseURL = baseURL
	}
	return session, nil
}

func Save(path string, session Session) error {
	now := time.Now().UTC()
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	if session.Environment == "" {
		session.Environment = ProductionEnv
	}
	baseURL, err := ResolveBaseURL(session.Environment, session.BaseURL)
	if err != nil {
		return err
	}
	session.BaseURL = baseURL

	b, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	b = append(b, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("save session: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure session permissions: %w", err)
	}
	return nil
}

func Delete(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func Redact(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return "****"
	}
	return value[:4] + "..." + value[len(value)-4:]
}
