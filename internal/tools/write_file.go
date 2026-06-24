package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/diff"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

type writeFileTool struct {
	ws *Workspace
}

func newWriteFileTool(ws *Workspace) Tool {
	return &writeFileTool{ws: ws}
}

func (t *writeFileTool) Name() string { return WriteFileToolName }

func (t *writeFileTool) RequiresConfirmation() bool { return true }

func (t *writeFileTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: WriteFileToolName,
		Description: "Writes content to a specified file in the local filesystem. " +
			"This tool completely OVERWRITES the file. You MUST provide the ENTIRE file content. NEVER use placeholders, truncation, or elision like '// ... existing code ...'. " +
			"The user has the ability to modify `content`. If modified, this will be stated in the response.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamFilePath: map[string]any{
					"type":        "string",
					"description": "The path to the file to write to.",
				},
				WriteFileParamContent: map[string]any{
					"type":        "string",
					"description": "The ENTIRE, complete content to write to the file. Do not truncate.",
				},
			},
			"required": []string{ParamFilePath, WriteFileParamContent},
		},
	}
}

func (t *writeFileTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := stringArg(args, ParamFilePath)
	if err != nil {
		return nil, err
	}
	contentRaw, ok := args[WriteFileParamContent]
	if !ok {
		return nil, fmt.Errorf("missing required parameter %q", WriteFileParamContent)
	}
	content, ok := contentRaw.(string)
	if !ok {
		return nil, fmt.Errorf("parameter %q must be a string", WriteFileParamContent)
	}
	if err := validateWriteFileContent(content); err != nil {
		return nil, err
	}
	abs, err := t.ws.ResolvePath(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("create parent directories: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}
	return map[string]any{
		"file_path": path,
		"status":    "ok",
	}, nil
}

func validateWriteFileContent(content string) error {
	if diff.LooksLikeEjectionMarker(content) {
		return fmt.Errorf("write_file content looks like a context ejection marker (<file_written ...>), not real file data. " +
			"That tag is metadata in conversation history — read the file with read_file (or reconstruct the code) and send the COMPLETE file body")
	}
	if diff.LooksLikePlaceholderContent(content) {
		return fmt.Errorf("write_file content contains a placeholder elision (e.g. \"... existing code ...\"). " +
			"This tool overwrites the entire file — read the file with read_file, then send the COMPLETE new contents. " +
			"Do not use placeholders or partial snippets")
	}
	if diff.LooksLikeUnifiedDiff(content) {
		return fmt.Errorf("write_file content looks like a unified diff (+/- edit lines or diff headers), not a complete file. " +
			"This tool is not a patch editor — read the file with read_file, then call write_file with the ENTIRE file body. " +
			"Never prefix lines with + or - and never copy diff previews from the UI")
	}
	return nil
}
