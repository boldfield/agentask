package tuiconfig

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigWithEnv(t *testing.T) {
	// Isolate from real filesystem
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	// Isolate from real filesystem
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	// Isolate from real filesystem
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	// Isolate from real filesystem
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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
	// Isolate from real filesystem
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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

func TestLoadConfigFromFile(t *testing.T) {
	// Create a temp directory for config
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("AGENTASK_URL", "")
	t.Setenv("AGENTASK_TOKEN", "")
	t.Setenv("AGENTASK_ACTOR", "")

	// Create config file
	configDir := filepath.Join(tempDir, "agentask")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
url = "http://fileserver:8080"
token = "file-token"
actor = "file-user"
poll_interval = "5s"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.URL != "http://fileserver:8080" {
		t.Errorf("expected URL http://fileserver:8080, got %s", cfg.URL)
	}

	if cfg.Token != "file-token" {
		t.Errorf("expected token file-token, got %s", cfg.Token)
	}

	if cfg.Actor != "file-user" {
		t.Errorf("expected actor file-user, got %s", cfg.Actor)
	}

	if cfg.PollInterval != 5*time.Second {
		t.Errorf("expected PollInterval 5s, got %v", cfg.PollInterval)
	}
}

func TestLoadConfigFilePrecedence(t *testing.T) {
	// Test that env > file and flags > env > file
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Create config file
	configDir := filepath.Join(tempDir, "agentask")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
url = "http://fileserver:8080"
token = "file-token"
actor = "file-user"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Test env > file
	t.Setenv("AGENTASK_URL", "http://envserver:8080")
	t.Setenv("AGENTASK_TOKEN", "env-token")
	t.Setenv("AGENTASK_ACTOR", "env-user")

	cfg, err := LoadConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.URL != "http://envserver:8080" {
		t.Errorf("env should override file: expected http://envserver:8080, got %s", cfg.URL)
	}

	// Test flags > env > file
	cfg, err = LoadConfig("http://flagserver:8080", "flag-token", "flag-user")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.URL != "http://flagserver:8080" {
		t.Errorf("flags should override env/file: expected http://flagserver:8080, got %s", cfg.URL)
	}

	if cfg.Token != "flag-token" {
		t.Errorf("flags should override env/file: expected flag-token, got %s", cfg.Token)
	}
}

func TestPollIntervalParsing(t *testing.T) {
	// Test valid poll_interval in file
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "token")

	configDir := filepath.Join(tempDir, "agentask")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
url = "http://localhost:8080"
token = "token"
poll_interval = "10s"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := LoadConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.PollInterval != 10*time.Second {
		t.Errorf("expected PollInterval 10s, got %v", cfg.PollInterval)
	}
}

func TestPollIntervalParseErrorWarning(t *testing.T) {
	// Test that invalid poll_interval generates a warning (not error)
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("AGENTASK_URL", "http://localhost:8080")
	t.Setenv("AGENTASK_TOKEN", "token")

	configDir := filepath.Join(tempDir, "agentask")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
url = "http://localhost:8080"
token = "token"
poll_interval = "invalid-duration"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cfg, err := LoadConfig("", "", "")

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("LoadConfig should not error on invalid poll_interval: %v", err)
	}

	// Should fall back to default
	if cfg.PollInterval != 2*time.Second {
		t.Errorf("expected default PollInterval 2s on parse error, got %v", cfg.PollInterval)
	}

	// Should have warned to stderr
	stderrOutput := buf.String()
	if stderrOutput == "" {
		t.Error("expected warning to stderr for invalid poll_interval")
	}
}

func TestWorldReadableFileWarning(t *testing.T) {
	// Test that a world-readable token file generates a warning
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("AGENTASK_URL", "")
	t.Setenv("AGENTASK_TOKEN", "")

	configDir := filepath.Join(tempDir, "agentask")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	configContent := `
url = "http://localhost:8080"
token = "world-readable-token"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	_, _ = LoadConfig("", "", "")

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)

	stderrOutput := buf.String()
	if stderrOutput == "" {
		t.Error("expected warning to stderr for world-readable config file")
	}
	if !bytes.Contains(buf.Bytes(), []byte("world-readable")) {
		t.Errorf("warning should mention world-readable: %s", stderrOutput)
	}
}
