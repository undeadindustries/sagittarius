package checks

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
)

const maxCheckOutputBytes = 6000

// Argv builds the final argument vector, narrowing file-scoped checks to
// the caller's paths by replacing the trailing default target.
func Argv(check Check, paths []string) []string {
	if !check.FileScoped || len(paths) == 0 || len(check.Args) == 0 {
		return check.Args
	}
	argv := make([]string, 0, len(check.Args)-1+len(paths))
	argv = append(argv, check.Args[:len(check.Args)-1]...)
	argv = append(argv, paths...)
	return argv
}

// Run executes a check and returns its success status, exit code, and truncated output.
func Run(ctx context.Context, dir string, check Check, argv []string) (ok bool, exitCode int, output string) {
	cmd := exec.CommandContext(ctx, check.Command, argv...)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	runErr := cmd.Run()
	out := strings.TrimSpace(buf.String())

	ok = true
	if runErr != nil {
		ok = false
		if exitErr, isExit := runErr.(*exec.ExitError); isExit {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
			if out != "" {
				out += "\n"
			}
			out += runErr.Error()
		}
	}
	// Some checks (e.g. `gofmt -l`) exit 0 but list offending files; treat any
	// output as a failure for those.
	if ok && check.FailOnOutput && out != "" {
		ok = false
	}

	if out == "" {
		out = "(empty)"
	}
	return ok, exitCode, Truncate(out)
}

// Truncate caps the length of a check's output.
func Truncate(s string) string {
	if len(s) <= maxCheckOutputBytes {
		return s
	}
	return s[:maxCheckOutputBytes] + "\n... (output truncated)"
}