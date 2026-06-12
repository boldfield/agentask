package localcommit

import (
	"testing"
)

func TestWorktreeHome(t *testing.T) {
	tests := []struct {
		name            string
		worktreeHomeEnv string
		homeEnv         string
		wantError       bool
		wantPath        string
	}{
		{
			name:            "WORKTREE_HOME wins over HOME",
			worktreeHomeEnv: "/home/user/worktree",
			homeEnv:         "/home/user",
			wantError:       false,
			wantPath:        "/home/user/worktree",
		},
		{
			name:            "HOME used as fallback",
			worktreeHomeEnv: "",
			homeEnv:         "/home/user",
			wantError:       false,
			wantPath:        "/home/user",
		},
		{
			name:            "neither set errors",
			worktreeHomeEnv: "",
			homeEnv:         "",
			wantError:       true,
		},
		{
			// The resolver no longer rejects tmpfs paths — that is the durability
			// guard's job (EnsureDurableWorktreeHome). It just resolves the value.
			name:            "tmpfs path resolves (guard is separate)",
			worktreeHomeEnv: "/tmp/worktree",
			homeEnv:         "/home/user",
			wantError:       false,
			wantPath:        "/tmp/worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTASK_WORKTREE_HOME", tt.worktreeHomeEnv)
			t.Setenv("AGENTASK_HOME", tt.homeEnv)

			got, err := WorktreeHome()

			if (err != nil) != tt.wantError {
				t.Errorf("WorktreeHome() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if !tt.wantError && got != tt.wantPath {
				t.Errorf("WorktreeHome() got %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestEnsureDurableWorktreeHome(t *testing.T) {
	tests := []struct {
		name            string
		worktreeHomeEnv string
		homeEnv         string
		wantError       bool
	}{
		{
			name:            "durable path passes",
			worktreeHomeEnv: "/home/user/worktree",
			homeEnv:         "/home/user",
			wantError:       false,
		},
		{
			name:            "neither set errors",
			worktreeHomeEnv: "",
			homeEnv:         "",
			wantError:       true,
		},
		{
			name:            "WORKTREE_HOME under /tmp errors",
			worktreeHomeEnv: "/tmp/worktree",
			homeEnv:         "/home/user",
			wantError:       true,
		},
		{
			name:            "HOME under /var/folders errors",
			worktreeHomeEnv: "",
			homeEnv:         "/var/folders/test",
			wantError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTASK_WORKTREE_HOME", tt.worktreeHomeEnv)
			t.Setenv("AGENTASK_HOME", tt.homeEnv)

			err := EnsureDurableWorktreeHome()
			if (err != nil) != tt.wantError {
				t.Errorf("EnsureDurableWorktreeHome() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}
