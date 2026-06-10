package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForgeTokenForOwner(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a test forge-tokens file
	forgeTokensPath := filepath.Join(tmpDir, "forge-tokens")
	testContent := `# This is a comment line
github=ghp_abc123
GITLAB=glpat_def456
bitbucket= ghp_quoted"quoted"
# inline comment here
factory=ghp_unquoted
missing-owner=ghp_xyz789
  whitespace  =  ghp_ws
quoted-single='ghp_single'
quoted-double="ghp_double"

blank-line-above=ghp_test
`

	err := os.WriteFile(forgeTokensPath, []byte(testContent), 0o600)
	if err != nil {
		t.Fatalf("failed to create test forge-tokens file: %v", err)
	}

	// Override HOME for testing
	oldHome := os.Getenv("HOME")
	defer func() {
		if oldHome != "" {
			os.Setenv("HOME", oldHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	// Mock home directory by creating symlink or just use a test wrapper
	// For simplicity, we'll use a wrapper function approach by calling the internal logic directly
	// But first, let's test via file system by setting HOME
	testHomeDir := filepath.Join(tmpDir, "home")
	os.MkdirAll(filepath.Join(testHomeDir, ".agentask"), 0o700)
	os.Rename(forgeTokensPath, filepath.Join(testHomeDir, ".agentask", "forge-tokens"))
	os.Setenv("HOME", testHomeDir)

	tests := []struct {
		owner    string
		expected string
		name     string
	}{
		{
			name:     "exact match",
			owner:    "github",
			expected: "ghp_abc123",
		},
		{
			name:     "case-insensitive match uppercase",
			owner:    "GITHUB",
			expected: "ghp_abc123",
		},
		{
			name:     "case-insensitive match mixed",
			owner:    "GitLab",
			expected: "glpat_def456",
		},
		{
			name:     "case-insensitive stored uppercase",
			owner:    "gitlab",
			expected: "glpat_def456",
		},
		{
			name:     "token with inline comment stripped",
			owner:    "bitbucket",
			expected: `ghp_quoted"quoted"`,
		},
		{
			name:     "unquoted token",
			owner:    "factory",
			expected: "ghp_unquoted",
		},
		{
			name:     "whitespace trimming",
			owner:    "whitespace",
			expected: "ghp_ws",
		},
		{
			name:     "single-quoted token",
			owner:    "quoted-single",
			expected: "ghp_single",
		},
		{
			name:     "double-quoted token",
			owner:    "quoted-double",
			expected: "ghp_double",
		},
		{
			name:     "missing owner",
			owner:    "nonexistent",
			expected: "",
		},
		{
			name:     "blank line above",
			owner:    "blank-line-above",
			expected: "ghp_test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := forgeTokenForOwner(tt.owner)
			if result != tt.expected {
				t.Errorf("forgeTokenForOwner(%q) = %q, want %q", tt.owner, result, tt.expected)
			}
		})
	}
}

func TestForgeTokenForOwnerMissingFile(t *testing.T) {
	// Override HOME to a directory without forge-tokens file
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".agentask"), 0o700)

	oldHome := os.Getenv("HOME")
	defer func() {
		if oldHome != "" {
			os.Setenv("HOME", oldHome)
		} else {
			os.Unsetenv("HOME")
		}
	}()

	os.Setenv("HOME", tmpDir)

	result := forgeTokenForOwner("any-owner")
	if result != "" {
		t.Errorf("forgeTokenForOwner with missing file should return empty string, got %q", result)
	}
}

func TestStripSurroundingQuotes(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		name     string
	}{
		{
			name:     "double quotes",
			input:    `"ghp_token"`,
			expected: "ghp_token",
		},
		{
			name:     "single quotes",
			input:    `'ghp_token'`,
			expected: "ghp_token",
		},
		{
			name:     "no quotes",
			input:    "ghp_token",
			expected: "ghp_token",
		},
		{
			name:     "mismatched quotes",
			input:    `"ghp_token'`,
			expected: `"ghp_token'`,
		},
		{
			name:     "single character",
			input:    "a",
			expected: "a",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "quotes only",
			input:    `""`,
			expected: "",
		},
		{
			name:     "quotes with embedded quotes",
			input:    `"outer"inner"quote"`,
			expected: `outer"inner"quote`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripSurroundingQuotes(tt.input)
			if result != tt.expected {
				t.Errorf("stripSurroundingQuotes(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
