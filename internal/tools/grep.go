package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

const (
	defaultTotalMaxMatches = 100
	grepTimeout            = 2 * time.Minute
)

type grepTool struct {
	ws *Workspace
}

func newGrepTool(ws *Workspace) Tool {
	return &grepTool{ws: ws}
}

func (t *grepTool) Name() string { return GrepToolName }

func (t *grepTool) RequiresConfirmation() bool { return false }

func (t *grepTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        GrepToolName,
		Description: "Searches for a regular expression pattern within file contents using ripgrep (rg).",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamPattern: map[string]any{
					"type":        "string",
					"description": "The pattern to search for.",
				},
				ParamDirPath: map[string]any{
					"type":        "string",
					"description": "Directory or file to search. Defaults to workspace root.",
				},
				GrepParamIncludePattern: map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g., '*.ts').",
				},
				GrepParamExcludePattern: map[string]any{
					"type":        "string",
					"description": "Regex pattern to exclude from results.",
				},
				GrepParamNamesOnly: map[string]any{
					"type":        "boolean",
					"description": "If true, return only file paths.",
				},
				ParamCaseSensitive: map[string]any{
					"type":        "boolean",
					"description": "Case-sensitive search.",
				},
				GrepParamFixedStrings: map[string]any{
					"type":        "boolean",
					"description": "Treat pattern as literal string.",
				},
				GrepParamContext: map[string]any{
					"type":    "integer",
					"minimum": 0,
				},
				GrepParamAfter: map[string]any{
					"type":    "integer",
					"minimum": 0,
				},
				GrepParamBefore: map[string]any{
					"type":    "integer",
					"minimum": 0,
				},
				GrepParamNoIgnore: map[string]any{
					"type": "boolean",
				},
				GrepParamMaxMatchesPerFile: map[string]any{
					"type":    "integer",
					"minimum": 1,
				},
				GrepParamTotalMaxMatches: map[string]any{
					"type":    "integer",
					"minimum": 1,
				},
			},
			"required": []string{ParamPattern},
		},
	}
}

func (t *grepTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pattern, err := stringArg(args, ParamPattern)
	if err != nil {
		return nil, err
	}

	searchPath := t.ws.Root()
	if dir := optionalStringArg(args, ParamDirPath); dir != "" {
		resolved, err := t.ws.ResolvePath(dir)
		if err != nil {
			return nil, err
		}
		searchPath = resolved
	}

	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("ripgrep (rg) not found in PATH")
	}

	rgArgs := []string{"--line-number", "--color=never", "--max-columns=500"}
	if include := optionalStringArg(args, GrepParamIncludePattern); include != "" {
		rgArgs = append(rgArgs, "-g", include)
	}
	if namesOnly, ok, err := boolArg(args, GrepParamNamesOnly); err != nil {
		return nil, err
	} else if ok && namesOnly {
		rgArgs = append(rgArgs, "--files-with-matches")
	}
	if fixed, ok, err := boolArg(args, GrepParamFixedStrings); err != nil {
		return nil, err
	} else if ok && fixed {
		rgArgs = append(rgArgs, "-F")
	}
	if sensitive, ok, err := boolArg(args, ParamCaseSensitive); err != nil {
		return nil, err
	} else if ok && sensitive {
		rgArgs = append(rgArgs, "-s")
	} else if !ok || !sensitive {
		rgArgs = append(rgArgs, "-i")
	}
	if noIgnore, ok, err := boolArg(args, GrepParamNoIgnore); err != nil {
		return nil, err
	} else if ok && noIgnore {
		rgArgs = append(rgArgs, "--no-ignore")
	}
	if ctxN, ok, err := intArg(args, GrepParamContext); err != nil {
		return nil, err
	} else if ok && ctxN > 0 {
		rgArgs = append(rgArgs, "-C", strconv.Itoa(ctxN))
	}
	if after, ok, err := intArg(args, GrepParamAfter); err != nil {
		return nil, err
	} else if ok && after > 0 {
		rgArgs = append(rgArgs, "-A", strconv.Itoa(after))
	}
	if before, ok, err := intArg(args, GrepParamBefore); err != nil {
		return nil, err
	} else if ok && before > 0 {
		rgArgs = append(rgArgs, "-B", strconv.Itoa(before))
	}
	if maxPerFile, ok, err := intArg(args, GrepParamMaxMatchesPerFile); err != nil {
		return nil, err
	} else if ok && maxPerFile > 0 {
		rgArgs = append(rgArgs, "--max-count", strconv.Itoa(maxPerFile))
	}
	totalMax := defaultTotalMaxMatches
	if maxTotal, ok, err := intArg(args, GrepParamTotalMaxMatches); err != nil {
		return nil, err
	} else if ok && maxTotal > 0 {
		totalMax = maxTotal
	}
	rgArgs = append(rgArgs, "--max-count", strconv.Itoa(totalMax))
	rgArgs = append(rgArgs, pattern, searchPath)

	runCtx, cancel := context.WithTimeout(ctx, grepTimeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, rgPath, rgArgs...)
	cmd.Dir = t.ws.Root()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	output := strings.TrimSpace(stdout.String())
	if output == "" && err == nil {
		output = "(no matches)"
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return map[string]any{
				"pattern": pattern,
				"matches": "(no matches)",
			}, nil
		}
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("grep failed: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("grep failed: %w", err)
	}

	lines := strings.Split(output, "\n")
	if len(lines) > totalMax {
		lines = lines[:totalMax]
	}

	relOutput := make([]string, 0, len(lines))
	for _, line := range lines {
		if idx := strings.Index(line, ":"); idx > 0 {
			filePart := line[:idx]
			if rel, err := filepath.Rel(t.ws.Root(), filePart); err == nil && !strings.HasPrefix(rel, "..") {
				line = rel + line[idx:]
			}
		}
		relOutput = append(relOutput, line)
	}

	return map[string]any{
		"pattern": pattern,
		"matches": strings.Join(relOutput, "\n"),
	}, nil
}
