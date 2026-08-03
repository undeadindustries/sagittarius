package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

type editTool struct {
	ws *Workspace
}

func newEditTool(ws *Workspace) Tool {
	return &editTool{ws: ws}
}

func (t *editTool) Name() string { return EditToolName }

func (t *editTool) Description() string {
	return `Replaces old_string with new_string in a file.
Prefer this over write_file for modifying existing files, as it is much faster and reduces truncation risks.
old_string must match exactly, including whitespace and indentation.`
}

func (t *editTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamFilePath: map[string]any{
					"type":        "string",
					"description": "The absolute or workspace-relative path to the file to edit.",
				},
				EditParamOldString: map[string]any{
					"type":        "string",
					"description": "The exact string to replace. Must match the file contents perfectly (including indentation). If the file does not exist, leave this empty to create it.",
				},
				EditParamNewString: map[string]any{
					"type":        "string",
					"description": "The string to replace old_string with.",
				},
				EditParamReplaceAll: map[string]any{
					"type":        "boolean",
					"description": "If true, replaces all occurrences of old_string. If false (default), the match must be unique in the file.",
				},
			},
			"required": []string{ParamFilePath, EditParamOldString, EditParamNewString},
		},
	}
}

func (t *editTool) RequiresConfirmation() bool { return true }

func (t *editTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	relPath, err := stringArg(args, ParamFilePath)
	if err != nil {
		return nil, err
	}
	absPath, err := t.ws.ResolvePath(relPath)
	if err != nil {
		return nil, err
	}

	oldStrRaw, ok := args[EditParamOldString]
	if !ok {
		return nil, fmt.Errorf("missing required parameter %q", EditParamOldString)
	}
	oldStr, ok := oldStrRaw.(string)
	if !ok {
		return nil, fmt.Errorf("parameter %q must be a string", EditParamOldString)
	}

	newStrRaw, ok := args[EditParamNewString]
	if !ok {
		return nil, fmt.Errorf("missing required parameter %q", EditParamNewString)
	}
	newStr, ok := newStrRaw.(string)
	if !ok {
		return nil, fmt.Errorf("parameter %q must be a string", EditParamNewString)
	}

	replaceAll := false
	if v, ok := args[EditParamReplaceAll].(bool); ok {
		replaceAll = v
	}

	if oldStr == newStr {
		return nil, fmt.Errorf("old_string and new_string are identical; no changes to make")
	}

	contentBytes, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if oldStr != "" {
				return nil, fmt.Errorf("file %q does not exist, but old_string is not empty. To create a new file, old_string must be empty", relPath)
			}
			// Create new file
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return nil, fmt.Errorf("create parent directories: %w", err)
			}
			if err := os.WriteFile(absPath, []byte(newStr), 0o644); err != nil {
				return nil, fmt.Errorf("failed to write new file: %w", err)
			}
			return map[string]any{
				"file_path": relPath,
				"status":    "ok",
				"message":   "File created successfully.",
			}, nil
		}
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	content := string(contentBytes)

	if content != "" && oldStr == "" {
		return nil, fmt.Errorf("old_string is empty but the file exists and is not empty. Use write_file if you intend to completely replace the file's contents, or provide the exact old_string to replace")
	}

	// Line ending normalization. Detect file's ending, and normalize oldStr and newStr.
	hasCRLF := strings.Contains(content, "\r\n")
	oldStr = normalizeLineEndings(oldStr, hasCRLF)
	newStr = normalizeLineEndings(newStr, hasCRLF)

	match, matchedOld, matchErr := findMatch(content, oldStr, replaceAll)
	if matchErr != nil {
		return nil, matchErr
	}
	if !match {
		return nil, fmt.Errorf("could not find old_string in the file. It must match exactly, including whitespace, indentation, and line endings. Read the file with read_file and retry with the exact text")
	}

	// Apply
	var newContent string
	if replaceAll {
		newContent = strings.ReplaceAll(content, matchedOld, newStr)
	} else {
		newContent = strings.Replace(content, matchedOld, newStr, 1)
	}

	if err := os.WriteFile(absPath, []byte(newContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	return map[string]any{
		"file_path": relPath,
		"status":    "ok",
		"message":   "File edited successfully.",
	}, nil
}

func normalizeLineEndings(s string, wantCRLF bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if wantCRLF {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func isDisproportionateMatch(oldStr, matched string) bool {
	if len(matched) < 20 {
		return false
	}
	if len(oldStr) > 0 && len(matched) > len(oldStr)*3 {
		return true
	}
	return false
}

func findMatch(content, oldStr string, replaceAll bool) (bool, string, error) {
	if oldStr == "" {
		return false, "", nil
	}

	// 1. Exact match
	countExact := strings.Count(content, oldStr)
	if countExact > 0 {
		if !replaceAll && countExact > 1 {
			return false, "", fmt.Errorf("found multiple matches for old_string. Provide more surrounding context to make the match unique")
		}
		return true, oldStr, nil
	}

	// 2. Line-trimmed match
	if matched, ok := tryTrimmedMatch(content, oldStr, replaceAll); ok {
		return true, matched, nil
	}

	// 3. Whitespace-normalized match
	if matched, ok := tryWhitespaceNormalizedMatch(content, oldStr, replaceAll); ok {
		return true, matched, nil
	}

	return false, "", fmt.Errorf("could not find old_string in the file. It must match exactly, including whitespace, indentation, and line endings. Read the file with read_file and retry with the exact text")
}

func tryTrimmedMatch(content, oldStr string, replaceAll bool) (string, bool) {
	normalize := func(s string) string {
		lines := strings.Split(s, "\n")
		var out []string
		for _, l := range lines {
			out = append(out, strings.TrimSpace(l))
		}
		return strings.Join(out, "\n")
	}

	normOld := normalize(oldStr)
	contentLines := strings.Split(content, "\n")
	oldLines := strings.Split(normOld, "\n")

	if len(oldLines) == 0 || len(contentLines) < len(oldLines) {
		return "", false
	}

	var matches []string
	for i := 0; i <= len(contentLines)-len(oldLines); i++ {
		match := true
		for j := 0; j < len(oldLines); j++ {
			if strings.TrimSpace(contentLines[i+j]) != oldLines[j] {
				match = false
				break
			}
		}
		if match {
			matches = append(matches, strings.Join(contentLines[i:i+len(oldLines)], "\n"))
		}
	}

	if len(matches) == 0 {
		return "", false
	}
	if !replaceAll && len(matches) > 1 {
		return "", false
	}

	uniqueMatch := matches[0]
	for _, m := range matches {
		if m != uniqueMatch {
			return "", false
		}
	}

	if isDisproportionateMatch(oldStr, uniqueMatch) {
		return "", false
	}

	return uniqueMatch, true
}

func tryWhitespaceNormalizedMatch(content, oldStr string, replaceAll bool) (string, bool) {
	fields := strings.Fields(oldStr)
	if len(fields) == 0 {
		return "", false
	}

	var parts []string
	for _, f := range fields {
		parts = append(parts, regexp.QuoteMeta(f))
	}
	pattern := strings.Join(parts, `\s+`)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", false
	}

	matches := re.FindAllString(content, -1)
	if len(matches) == 0 {
		return "", false
	}
	if !replaceAll && len(matches) > 1 {
		return "", false
	}

	uniqueMatch := matches[0]
	for _, m := range matches {
		if m != uniqueMatch {
			return "", false
		}
	}

	if isDisproportionateMatch(oldStr, uniqueMatch) {
		return "", false
	}

	return uniqueMatch, true
}
