package localcommit

import (
	"os/exec"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		// Basic cases
		{"hello world", "hello-world"},
		{"hello_world", "hello-world"},
		{"hello-world", "hello-world"},

		// Spaces and underscores
		{"foo bar baz", "foo-bar-baz"},
		{"foo__bar", "foo-bar"},
		{"foo  bar", "foo-bar"},

		// Unicode and special characters
		{"café résumé", "caf-rsum"},
		{"hello@world#test", "helloworldtest"},
		{"my-new-feature", "my-new-feature"},

		// Empty and whitespace only
		{"", "item"},
		{"   ", "item"},

		// Already slug-like
		{"already-slug", "already-slug"},

		// Repeated dashes
		{"foo---bar", "foo-bar"},
		{"hello--world--test", "hello-world-test"},

		// Leading/trailing dashes
		{"-hello-", "hello"},
		{"--world--", "world"},

		// Mixed cases
		{"MY AWESOME Feature!", "my-awesome-feature"},
		{"test_case_123", "test-case-123"},
	}

	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			got := Slugify(tt.title)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestBaseRef(t *testing.T) {
	got := BaseRef()
	want := "origin/main"
	if got != want {
		t.Errorf("BaseRef() = %q, want %q", got, want)
	}
}

func TestMRBranch(t *testing.T) {
	got := MRBranch("my-feature")
	want := "wi/my-feature"
	if got != want {
		t.Errorf("MRBranch(%q) = %q, want %q", "my-feature", got, want)
	}
}

func TestWIPBranch(t *testing.T) {
	got := WIPBranch("task-123")
	want := "wip/task-123"
	if got != want {
		t.Errorf("WIPBranch(%q) = %q, want %q", "task-123", got, want)
	}
}

func TestResolveTip(t *testing.T) {
	t.Run("branch exists", func(t *testing.T) {
		// Create a temporary git repo
		tmpDir := t.TempDir()

		// Initialize git repo
		cmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@example.com"},
			{"git", "config", "user.name", "Test User"},
			{"git", "commit", "--allow-empty", "-m", "initial"},
			{"git", "branch", "wi/my-feature"},
		}

		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		// Test that ResolveTip finds the branch
		got, err := ResolveTip(tmpDir, "my-feature")
		if err != nil {
			t.Errorf("ResolveTip() error = %v", err)
		}
		want := "wi/my-feature"
		if got != want {
			t.Errorf("ResolveTip() = %q, want %q", got, want)
		}
	})

	t.Run("branch does not exist", func(t *testing.T) {
		// Create a temporary git repo
		tmpDir := t.TempDir()

		// Initialize git repo
		cmds := [][]string{
			{"git", "init"},
			{"git", "config", "user.email", "test@example.com"},
			{"git", "config", "user.name", "Test User"},
			{"git", "commit", "--allow-empty", "-m", "initial"},
		}

		for _, args := range cmds {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Dir = tmpDir
			if err := cmd.Run(); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
		}

		// Test that ResolveTip returns BaseRef when branch doesn't exist
		got, err := ResolveTip(tmpDir, "nonexistent")
		if err != nil {
			t.Errorf("ResolveTip() error = %v", err)
		}
		want := "origin/main"
		if got != want {
			t.Errorf("ResolveTip() = %q, want %q", got, want)
		}
	})
}
