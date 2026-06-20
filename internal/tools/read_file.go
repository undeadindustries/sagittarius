package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

type readFileTool struct {
	ws *Workspace
}

func newReadFileTool(ws *Workspace) Tool {
	return &readFileTool{ws: ws}
}

func (t *readFileTool) Name() string { return ReadFileToolName }

func (t *readFileTool) RequiresConfirmation() bool { return false }

func (t *readFileTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: ReadFileToolName,
		Description: "Reads and returns the content of a specified file. If the file is large, the content will be truncated. " +
			"The tool's response will clearly indicate if truncation has occurred and will provide details on how to read more " +
			"of the file using the 'start_line' and 'end_line' parameters.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamFilePath: map[string]any{
					"type":        "string",
					"description": "The path to the file to read.",
				},
				ReadFileParamStartLine: map[string]any{
					"type":        "integer",
					"description": "Optional: The 1-based line number to start reading from.",
					"minimum":     1,
				},
				ReadFileParamEndLine: map[string]any{
					"type":        "integer",
					"description": "Optional: The 1-based line number to end reading at (inclusive).",
					"minimum":     1,
				},
			},
			"required": []string{ParamFilePath},
		},
	}
}

func (t *readFileTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := stringArg(args, ParamFilePath)
	if err != nil {
		return nil, err
	}
	abs, err := t.ws.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	content := string(data)
	start, hasStart, err := intArg(args, ReadFileParamStartLine)
	if err != nil {
		return nil, err
	}
	end, hasEnd, err := intArg(args, ReadFileParamEndLine)
	if err != nil {
		return nil, err
	}
	if hasStart || hasEnd {
		lines := strings.Split(content, "\n")
		if hasStart && start < 1 {
			return nil, fmt.Errorf("start_line must be >= 1")
		}
		if hasEnd && end < 1 {
			return nil, fmt.Errorf("end_line must be >= 1")
		}
		from := 1
		if hasStart {
			from = start
		}
		to := len(lines)
		if hasEnd {
			to = end
		}
		if from > len(lines) {
			content = ""
		} else {
			if to > len(lines) {
				to = len(lines)
			}
			if from > to {
				return nil, fmt.Errorf("start_line cannot be greater than end_line")
			}
			content = strings.Join(lines[from-1:to], "\n")
		}
	}
	return map[string]any{
		"file_path": path,
		"content":   content,
	}, nil
}
