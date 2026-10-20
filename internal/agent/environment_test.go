package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// initGitRepo makes a repository with one commit, skipping when git is absent.
func initGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "trunk")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "first commit")
	return dir
}

func TestCaptureEnvironmentGitRepo(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/usr/bin/zsh")

	env := captureEnvironment(t.Context(), dir)
	if env.Date != time.Now().Format(envDateLayout) {
		t.Errorf("Date = %q, want today", env.Date)
	}
	if env.WorkDir != dir || env.Shell != "zsh" {
		t.Errorf("WorkDir/Shell = %q/%q, want %q/zsh", env.WorkDir, env.Shell, dir)
	}
	if env.Branch != "trunk" {
		t.Errorf("Branch = %q, want trunk", env.Branch)
	}
	if !strings.Contains(env.Status, "?? new.txt") {
		t.Errorf("Status = %q, want untracked new.txt", env.Status)
	}
	if !strings.Contains(env.RecentCommits, "first commit") {
		t.Errorf("RecentCommits = %q, want the first commit", env.RecentCommits)
	}
}

func TestCaptureEnvironmentNotARepo(t *testing.T) {
	env := captureEnvironment(t.Context(), t.TempDir())
	if env.Branch != "" || env.Status != "" || env.RecentCommits != "" {
		t.Errorf("git fields must be empty outside a repo: %+v", env)
	}
	if env.Date == "" {
		t.Error("Date must be set outside a repo")
	}
}

func TestSplitBranchStatus(t *testing.T) {
	t.Parallel()
	long := strings.Repeat(" M some/long/path/file.go\n", 40)
	tests := []struct {
		name, in, branch string
		wantStatus       func(string) bool
	}{
		{"tracking", "## main...origin/main [ahead 1]\n M a.go\n", "main",
			func(s string) bool { return s == " M a.go" }},
		{"clean", "## main\n", "main", func(s string) bool { return s == "" }},
		{"no commits", "## No commits yet on dev\n?? x\n", "dev",
			func(s string) bool { return s == "?? x" }},
		{"truncated", "## main\n" + long, "main", func(s string) bool {
			body, marker, ok := strings.Cut(s, "\n"+envStatusTruncated)
			return ok && marker == "" && len(body) <= envStatusMaxBytes && strings.HasSuffix(body, "file.go")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch, status := splitBranchStatus(tt.in)
			if branch != tt.branch {
				t.Errorf("branch = %q, want %q", branch, tt.branch)
			}
			if !tt.wantStatus(status) {
				t.Errorf("status = %q", status)
			}
		})
	}
}

func TestSubagentInheritsParentEnvironment(t *testing.T) {
	h, child := resultTestChild(t, config.SubagentResearch)
	if parent := h.parent.environment(); child.runner.environment() != parent {
		t.Fatalf("child environment = %p, want the parent's %p", child.runner.environment(), parent)
	}
}

// TestEnvironmentSnapshotStableAcrossRebuilds pins the cache contract: the
// working tree changing mid-session must not change the system prompt.
func TestEnvironmentSnapshotStableAcrossRebuilds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := initGitRepo(t)

	runner, err := NewRunner(RunnerConfig{
		Generator: &fakeGenerator{},
		Model:     "test-model",
		WorkDir:   dir,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.rebuildBasePrompt()
	runner.applyModeSystemSuffix()
	before := systemInstruction(t, runner)
	if !strings.Contains(before, "## Environment") || !strings.Contains(before, "- Git branch: trunk") {
		t.Fatalf("system prompt missing environment snapshot:\n%s", before)
	}

	if err := os.WriteFile(filepath.Join(dir, "later.txt"), []byte("l\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner.rebuildBasePrompt()
	runner.applyModeSystemSuffix()
	if after := systemInstruction(t, runner); after != before {
		t.Fatalf("system prompt changed after a working-tree edit; the snapshot must be frozen")
	}
}
