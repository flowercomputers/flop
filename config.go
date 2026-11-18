package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultAPIBaseURL = "https://api.bog.run"
)

type Config struct {
	URLs        URLConfig
	LocalConfig LocalConfig
}

type URLConfig struct {
	APIBaseURL string
}

type LocalConfig struct {
	APIKey           string
	ClientID         string
	PrincipalID      string
	OrganizationID   string
	OrganizationName string
	LastLogin        time.Time
}

func configDir() (string, error) {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "flop"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".flop"), nil
}

// apiBaseURL is the spade flop talks to: production's, or FLOP_API_BASE_URL
// (dev's is https://api.dev.bog.run)
func apiBaseURL() string {
	if u := os.Getenv("FLOP_API_BASE_URL"); u != "" {
		return u
	}
	return DefaultAPIBaseURL
}

// configFile is where the login for that spade is kept: config.json for
// production's, config-<host>.json for any other, so one machine can be
// signed in to production and dev at once
func configFile(dir string) string {
	u, err := url.Parse(apiBaseURL())
	if err != nil || apiBaseURL() == DefaultAPIBaseURL || u.Host == "" {
		return filepath.Join(dir, "config.json")
	}
	return filepath.Join(dir, "config-"+strings.ReplaceAll(u.Host, ":", "_")+".json")
}

func LoadConfig() (Config, error) {
	apiBaseURL := apiBaseURL()

	localConfig, err := LoadLocalConfig()
	if err != nil {
		return Config{}, fmt.Errorf("load local config: %w", err)
	}
	// CI signs in with an organization token (made in the panel) instead of a
	// login: FLOP_TOKEN and FLOP_ORGANIZATION_ID, both or neither
	if token := os.Getenv("FLOP_TOKEN"); token != "" {
		organization := os.Getenv("FLOP_ORGANIZATION_ID")
		if organization == "" {
			return Config{}, errors.New("FLOP_TOKEN needs FLOP_ORGANIZATION_ID: the organization the token belongs to")
		}
		localConfig = LocalConfig{APIKey: token, OrganizationID: organization}
	}
	return Config{
		URLs: URLConfig{
			APIBaseURL: apiBaseURL,
		},
		LocalConfig: localConfig,
	}, nil
}

func LoadLocalConfig() (LocalConfig, error) {
	dir, err := configDir()
	if err != nil {
		return LocalConfig{}, err
	}
	b, err := os.ReadFile(configFile(dir))
	if errors.Is(err, os.ErrNotExist) {
		return LocalConfig{}, nil // not logged in, not an error
	}
	if err != nil {
		return LocalConfig{}, fmt.Errorf("read config: %w", err)
	}
	var c LocalConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return LocalConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return c, nil
}

func SaveLocalConfig(c LocalConfig) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile(dir), b, 0o600)
}
