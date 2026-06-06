package tuiconfig

import (
	"testing"
	"time"
)

func TestLoadConfigWithEnv(t *testing.T) {
	// Set environment variables
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "env-token")
	t.Setenv("AGENTASK_ACTOR", "test-user")

	cfg, err := LoadConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.URL != "http://localhost:8080" {
		t.Errorf("expected URL http://localhost:8080, got %s", cfg.URL)
	}

	if cfg.Token != "env-token" {
		t.Errorf("expected token env-token, got %s", cfg.Token)
	}

	if cfg.Actor != "test-user" {
		t.Errorf("expected actor test-user, got %s", cfg.Actor)
	}

	if cfg.PollInterval != 2*time.Second {
		t.Errorf("expected PollInterval 2s, got %v", cfg.PollInterval)
	}
}

func TestLoadConfigWithFlags(t *testing.T) {
	// Set environment variables
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "env-token")

	// Flags should override env
	cfg, err := LoadConfig("http://flagserver:9000", "flag-token", "flag-user")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.URL != "http://flagserver:9000" {
		t.Errorf("expected URL http://flagserver:9000, got %s", cfg.URL)
	}

	if cfg.Token != "flag-token" {
		t.Errorf("expected token flag-token, got %s", cfg.Token)
	}

	if cfg.Actor != "flag-user" {
		t.Errorf("expected actor flag-user, got %s", cfg.Actor)
	}
}

func TestLoadConfigMissingRequired(t *testing.T) {
	// Clear environment
	t.Setenv("AGENTASK_URL", "")
	t.Setenv("AGENTASK_TOKEN", "")
	t.Setenv("AGENTASK_ACTOR", "")

	_, err := LoadConfig("", "", "")
	if err == nil {
		t.Fatal("expected error for missing URL")
	}

	if err.Error() != "missing AGENTASK_URL (set via config file, AGENTASK_URL env, or --url flag)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadConfigMissingToken(t *testing.T) {
	// Clear environment and set only URL
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "")
	t.Setenv("AGENTASK_ACTOR", "")

	_, err := LoadConfig("", "", "")
	if err == nil {
		t.Fatal("expected error for missing token")
	}

	if err.Error() != "missing AGENTASK_TOKEN (set via config file, AGENTASK_TOKEN env, or --token flag)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestActorDefaultsToUser(t *testing.T) {
	// Set required config without actor
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "token")
	t.Setenv("AGENTASK_ACTOR", "")

	cfg, err := LoadConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Actor should be set to $USER
	if cfg.Actor == "" {
		t.Error("expected Actor to be set to current user")
	}
}
