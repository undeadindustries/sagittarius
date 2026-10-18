package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools/checks"
)

const (
	projectChecksTimeout  = 5 * time.Minute
	projectChecksToolDesc = "Detects the project's language stack (Go, Node/TypeScript, Python, Rust) " +
		"from root marker files and runs its read-only verification checks (lint, format check, type check, " +
		"build, test). Returns a structured report of each check's pass/fail plus any missing tools with an " +
		"install hint. Use after editing code to confirm it is clean before declaring a task done. " +
		"Set fix=true only when the user wants formatters/auto-fixers to rewrite files."
)

type projectChecksTool struct {
	ws       *Workspace
	allowFix bool
}

func newProjectChecksTool(ws *Workspace, allowFix bool) Tool {
	return &projectChecksTool{ws: ws, allowFix: allowFix}
}

func (t *projectChecksTool) Name() string { return ProjectChecksToolName }

// RequiresConfirmation is false: check-only runs are read-only, and mutating
// fix runs are gated by the allowFix config and the plan/ask mode denial rather
// than a per-call prompt.
func (t *projectChecksTool) RequiresConfirmation() bool { return false }

func (t *projectChecksTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        ProjectChecksToolName,
		Description: projectChecksToolDesc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ProjectChecksParamPaths: map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional: limit file-scoped checks (lint, format, typecheck) to these workspace-relative files or directories. Each check only gets the file types it reads (mypy never sees pyproject.toml); a check with no matching paths is reported as skipped. Build, vet, and test still run on the whole module.",
				},
				ProjectChecksParamFix: map[string]any{
					"type":        "boolean",
					"description": "Optional: run mutating formatters/auto-fixers instead of check-only. Disabled unless allowed by configuration; not permitted in plan or ask mode.",
				},
			},
		},
	}
}

func (t *projectChecksTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fix, _, err := boolArg(args, ProjectChecksParamFix)
	if err != nil {
		return nil, err
	}
	if fix && !t.allowFix {
		return map[string]any{
			"error": "fix mode is disabled; set \"sagittarius.verify.allowFix\" to true to allow " +
				"run_project_checks to rewrite files, or run the formatter manually via run_shell_command",
		}, nil
	}

	paths, err := t.scopePaths(args)
	if err != nil {
		return nil, err
	}

	plan := checks.Detect(t.ws.Root(), fix)
	if plan.Stack == "" {
		return map[string]any{
			"stack":   "",
			"checks":  []any{},
			"all_ok":  true,
			"message": "no recognized project stack (go.mod, package.json, pyproject.toml, Cargo.toml); use run_shell_command for project-specific checks",
		}, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, projectChecksTimeout)
	defer cancel()

	results := make([]any, 0, len(plan.Checks))
	missing := make([]any, 0)
	seenMissing := make(map[string]bool)
	allOK := true

	for _, check := range plan.Checks {
		targets := paths
		if check.FileScoped && len(paths) > 0 {
			targets = checks.ScopePaths(check, paths, t.isDir)
			if len(targets) == 0 {
				results = append(results, map[string]any{
					"name":    check.Name,
					"command": check.Command,
					"ok":      true,
					"skipped": "none of the given paths are " + strings.Join(check.Extensions, "/") + " files or directories",
				})
				continue
			}
		}
		if _, lookErr := exec.LookPath(check.Command); lookErr != nil {
			if !seenMissing[check.Command] {
				seenMissing[check.Command] = true
				missing = append(missing, map[string]any{
					"tool":         check.Command,
					"install_hint": checks.InstallHint(check.Command),
				})
			}
			continue
		}

		argv := checks.Argv(check, targets)
		ok, exitCode, output := checks.Run(runCtx, t.ws.Root(), check, argv)
		if !ok {
			allOK = false
		}
		entry := map[string]any{
			"name":    check.Name,
			"command": strings.Join(append([]string{check.Command}, argv...), " "),
			"ok":      ok,
			"output":  output,
		}
		if exitCode != 0 {
			entry["exit_code"] = exitCode
		}
		results = append(results, entry)
	}

	return map[string]any{
		"stack":         plan.Stack,
		"checks":        results,
		"missing_tools": missing,
		"all_ok":        allOK,
	}, nil
}

// isDir reports whether a workspace-relative path is an existing directory, so
// checks.ScopePaths keeps directory targets for every file-scoped tool. Paths
// were validated by scopePaths; a resolve or stat failure means "not a
// directory", leaving the extension filter to decide.
func (t *projectChecksTool) isDir(rel string) bool {
	abs, err := t.ws.ResolvePath(rel)
	if err != nil {
		return false
	}
	info, err := os.Stat(abs)
	return err == nil && info.IsDir()
}

// scopePaths validates that any caller-supplied paths stay within the workspace
// and returns them as given (relative) for use as command targets.
func (t *projectChecksTool) scopePaths(args map[string]any) ([]string, error) {
	raw, ok := args[ProjectChecksParamPaths]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parameter %q must be an array of strings", ProjectChecksParamPaths)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("parameter %q must contain non-empty strings", ProjectChecksParamPaths)
		}
		// Reject paths that would be parsed as flags (e.g. "--fix"). Otherwise a
		// caller could inject a mutating flag through the path list and bypass
		// the allowFix gate and the plan/ask read-only restrictions. A real file
		// whose name starts with "-" can still be passed as "./-name".
		if strings.HasPrefix(s, "-") {
			return nil, fmt.Errorf("parameter %q entry %q must not start with %q; prefix it with \"./\" to target such a path", ProjectChecksParamPaths, s, "-")
		}
		if _, err := t.ws.ResolvePath(s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
