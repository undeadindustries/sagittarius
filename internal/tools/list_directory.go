package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

type listDirectoryTool struct {
	ws *Workspace
}

func newListDirectoryTool(ws *Workspace) Tool {
	return &listDirectoryTool{ws: ws}
}

func (t *listDirectoryTool) Name() string { return ListDirectoryToolName }

func (t *listDirectoryTool) RequiresConfirmation() bool { return false }

func (t *listDirectoryTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: ListDirectoryToolName,
		Description: "Lists the names of files and subdirectories directly within a specified directory path. " +
			"Can optionally ignore entries matching provided glob patterns.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamDirPath: map[string]any{
					"type":        "string",
					"description": "The path to the directory to list",
				},
				ListDirParamIgnore: map[string]any{
					"type":        "array",
					"description": "List of glob patterns to ignore",
					"items":       map[string]any{"type": "string"},
				},
			},
			"required": []string{ParamDirPath},
		},
	}
}

func (t *listDirectoryTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir, err := stringArg(args, ParamDirPath)
	if err != nil {
		return nil, err
	}
	abs, err := t.ws.ResolvePath(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, fmt.Errorf("list directory: %w", err)
	}
	ignores := stringSliceArg(args, ListDirParamIgnore)
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if shouldIgnore(name, ignores) {
			continue
		}
		if entry.IsDir() {
			name += string(os.PathSeparator)
		}
		names = append(names, name)
	}
	return map[string]any{
		"dir_path": dir,
		"entries":  names,
	}, nil
}

func stringSliceArg(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func shouldIgnore(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
		if strings.Contains(pattern, "*") {
			if matched, _ := filepath.Match(pattern, name); matched {
				return true
			}
		}
	}
	return false
}
