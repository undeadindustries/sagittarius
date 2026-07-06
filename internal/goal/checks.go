package goal

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"golang.org/x/sync/errgroup"
)

// runDeterministicChecks parses the objective for commands enclosed in backticks
// and runs them, returning their output to inform the evaluator.
func runDeterministicChecks(ctx context.Context, objective, workDir string) (string, error) {
	commands := extractCommands(objective)
	if len(commands) == 0 {
		return "", nil
	}

	var g errgroup.Group
	results := make([]string, len(commands))

	for i, cmdStr := range commands {
		i, cmdStr := i, cmdStr
		g.Go(func() error {
			out, err := runCommand(ctx, cmdStr, workDir)
			if err != nil {
				// We don't return err here because a failing command (e.g. failing test)
				// is valid ground truth, not an infrastructure error.
				results[i] = fmt.Sprintf("Command: %s\nError: %v\nOutput:\n%s", cmdStr, err, out)
			} else {
				results[i] = fmt.Sprintf("Command: %s\nSuccess\nOutput:\n%s", cmdStr, out)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return "", err
	}

	return strings.Join(results, "\n\n"), nil
}

func extractCommands(text string) []string {
	var cmds []string
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(text, -1)
	for _, m := range matches {
		cmd := strings.TrimSpace(m[1])
		if isSafeDeterministicCheck(cmd) {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func isSafeDeterministicCheck(cmd string) bool {
	// Only allow known read-only verification commands to avoid arbitrary execution
	// from backticks in the prompt.
	allowedPrefixes := []string{
		"npm test",
		"yarn test",
		"pnpm test",
		"go test",
		"make test",
		"make build",
		"cargo test",
		"cargo check",
		"pytest",
		"run_project_checks",
		"git status",
		"git diff",
	}
	for _, p := range allowedPrefixes {
		if strings.HasPrefix(cmd, p) {
			return true
		}
	}
	return false
}

func runCommand(ctx context.Context, cmdStr, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	// Truncate output to avoid blowing up context
	outStr := string(out)
	if len(outStr) > 4000 {
		outStr = outStr[len(outStr)-4000:] + "\n... (truncated)"
	}
	return outStr, err
}
