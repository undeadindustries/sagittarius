package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/prompt"
)

const (
	// envGitTimeout bounds every git call made for the environment snapshot.
	// It runs on the prompt-build path at session start, so a slow or wedged
	// repository must cost at most this much startup latency.
	envGitTimeout = 2 * time.Second
	// envStatusMaxBytes caps the git status carried in the system prompt. A
	// large dirty tree would otherwise put thousands of tokens into every
	// request of the session.
	envStatusMaxBytes = 500
	// envRecentCommits is how many one-line commit subjects the snapshot keeps.
	envRecentCommits   = 5
	envStatusTruncated = "... (truncated; run `git status` for the full list)"
	envDateLayout      = "2006-01-02"
)

// captureEnvironment snapshots what the model would otherwise rediscover with
// its first tool calls: date, working directory, shell, and git state. It is
// called once per session and the result is reused verbatim, because the
// system prompt is the provider's cache prefix. Git failures leave the git
// fields empty; the snapshot never fails.
func captureEnvironment(ctx context.Context, workDir string) prompt.Environment {
	env := prompt.Environment{
		Date:    time.Now().Format(envDateLayout),
		WorkDir: workDir,
	}
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		env.Shell = filepath.Base(shell)
	}
	if workDir == "" || !isGitRepo(workDir) {
		return env
	}

	ctx, cancel := context.WithTimeout(ctx, envGitTimeout)
	defer cancel()

	status, err := runEnvGit(ctx, workDir, "status", "--short", "--branch")
	if err != nil {
		slog.Debug("environment snapshot: git status failed", "dir", workDir, "err", err)
		return env
	}
	env.Branch, env.Status = splitBranchStatus(status)

	commits, err := runEnvGit(ctx, workDir, "log", "--oneline", fmt.Sprintf("-%d", envRecentCommits))
	if err != nil {
		// A repository with no commits yet fails here; branch and status still help.
		slog.Debug("environment snapshot: git log failed", "dir", workDir, "err", err)
		return env
	}
	env.RecentCommits = strings.TrimRight(commits, "\n")
	return env
}

// runEnvGit runs one read-only git command in dir and returns its stdout.
func runEnvGit(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir, "--no-optional-locks"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(out), nil
}

// splitBranchStatus parses `git status --short --branch`: the "## " header
// line names the branch (tracking info after "..." is dropped) and the rest is
// the short status, capped at envStatusMaxBytes.
func splitBranchStatus(out string) (branch, status string) {
	out = strings.TrimRight(out, "\n")
	header, rest, _ := strings.Cut(out, "\n")
	if name, ok := strings.CutPrefix(header, "## "); ok {
		name, _, _ = strings.Cut(name, "...")
		name = strings.TrimPrefix(name, "No commits yet on ")
		branch = strings.TrimSpace(name)
	} else {
		rest = out
	}
	return branch, capStatus(rest)
}

// capStatus truncates status at the last whole line within envStatusMaxBytes.
func capStatus(status string) string {
	if len(status) <= envStatusMaxBytes {
		return status
	}
	cut := status[:envStatusMaxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut + "\n" + envStatusTruncated
}
