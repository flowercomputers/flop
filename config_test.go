package main

import (
	"path/filepath"
	"testing"
)

// one login per spade: production's in config.json, any other's beside it
func TestALoginPerSpade(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	for _, c := range []struct{ api, file string }{
		{"", "config.json"},
		{DefaultAPIBaseURL, "config.json"}, // written last of the two
		{"https://api.dev.bog.run", "config-api.dev.bog.run.json"},
		{"http://localhost:8080", "config-localhost_8080.json"},
	} {
		api, file := c.api, c.file
		t.Setenv("FLOP_API_BASE_URL", api)
		if got := configFile(filepath.Join(dir, "flop")); filepath.Base(got) != file {
			t.Errorf("%q: %s, want %s", api, filepath.Base(got), file)
		}
		if err := SaveLocalConfig(LocalConfig{OrganizationName: api}); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FLOP_API_BASE_URL", "https://api.dev.bog.run")
	if c, err := LoadLocalConfig(); err != nil || c.OrganizationName != "https://api.dev.bog.run" {
		t.Fatalf("%+v %v", c, err)
	}
	t.Setenv("FLOP_API_BASE_URL", "")
	if c, err := LoadLocalConfig(); err != nil || c.OrganizationName != DefaultAPIBaseURL {
		t.Fatalf("production's: %+v %v", c, err)
	}
}

func TestAOrganizationTokenFromTheEnvironment(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FLOP_TOKEN", "fp_secret")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("FLOP_TOKEN without FLOP_ORGANIZATION_ID was accepted")
	}
	t.Setenv("FLOP_ORGANIZATION_ID", "ws-1")
	cfg, err := LoadConfig()
	if err != nil || cfg.LocalConfig.APIKey != "fp_secret" || cfg.LocalConfig.OrganizationID != "ws-1" {
		t.Fatalf("%v %+v", err, cfg.LocalConfig)
	}
}
