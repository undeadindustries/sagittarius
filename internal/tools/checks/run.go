package checks

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// ScopePaths narrows caller paths to those check can read, so a file-scoped
// tool never receives a file type it would misparse (mypy treats every
// argument as Python, so pyproject.toml becomes a syntax error). Directories
// always pass: the tool walks them with its own file discovery. isDir reports
// whether a path is a directory; it is injected so this stays free of I/O.
// Checks that are not file-scoped, or that accept every file, get paths back
// unchanged. An empty result means none of the paths apply to this check.
func ScopePaths(check Check, paths []string, isDir func(string) bool) []string {
	if !check.FileScoped || len(check.Extensions) == 0 {
		return paths
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		if slices.Contains(check.Extensions, ext) || (isDir != nil && isDir(p)) {
			out = append(out, p)
		}
	}
	return out
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
