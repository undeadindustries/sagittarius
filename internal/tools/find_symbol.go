package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/symbols"
)

const (
	// findSymbolDefaultMaxResults caps returned tags when the caller does not
	// specify max_results (mirrors grep's defaultTotalMaxMatches).
	findSymbolDefaultMaxResults = 150
	// findSymbolMaxScanFiles bounds a single directory scan so an ad-hoc call
	// stays fast and cheap; there is no persistent index to amortize over.
	findSymbolMaxScanFiles = 500
	// findSymbolMaxFileBytes skips files larger than this (generated bundles,
	// minified assets) that would dominate a scan without adding useful symbols.
	findSymbolMaxFileBytes = 1 << 20 // 1 MiB
	// findSymbolTimeout bounds the whole call, including file enumeration.
	findSymbolTimeout = 2 * time.Minute
)

const (
	findSymbolKindDefinition = "definition"
	findSymbolKindReference  = "reference"
	findSymbolKindAll        = "all"
)

type findSymbolTool struct {
	ws          *Workspace
	preferGopls bool
	// isGoModule is resolved once at construction so Declaration stays static;
	// it only tweaks the description's gopls note.
	isGoModule bool
}

func newFindSymbolTool(ws *Workspace, preferGopls bool) Tool {
	t := &findSymbolTool{ws: ws, preferGopls: preferGopls}
	if _, err := os.Stat(filepath.Join(ws.Root(), "go.mod")); err == nil {
		t.isGoModule = true
	}
	return t
}

func (t *findSymbolTool) Name() string { return FindSymbolToolName }

func (t *findSymbolTool) RequiresConfirmation() bool { return false }

func (t *findSymbolTool) Declaration() provider.ToolDeclaration {
	desc := "Finds where code symbols (functions, types, classes, methods, etc.) are " +
		"defined or referenced across source files, using a syntax-aware parser. " +
		"Prefer this over grep_search when you know a symbol name and want its " +
		"definition or call sites. With no 'symbol', it returns an outline of all " +
		"definitions in the given file or directory. Works on source code across " +
		"many languages; it does not analyze prose or plain-text files."
	if t.preferGopls && t.isGoModule {
		desc += " For this Go module, connected gopls MCP tools (mcp_gopls_*) give more " +
			"precise, type-aware results; this tool is the dependency-free fallback."
	}
	return provider.ToolDeclaration{
		Name:        FindSymbolToolName,
		Description: desc,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ParamDirPath: map[string]any{
					"type":        "string",
					"description": "File or directory to search. Defaults to the workspace root.",
				},
				FindSymbolParamSymbol: map[string]any{
					"type":        "string",
					"description": "Symbol name to match (case-insensitive substring). Omit to outline all definitions.",
				},
				FindSymbolParamKind: map[string]any{
					"type":        "string",
					"description": "Which occurrences to return: 'definition', 'reference', or 'all'. Defaults to 'definition'.",
					"enum":        []string{findSymbolKindDefinition, findSymbolKindReference, findSymbolKindAll},
				},
				FindSymbolParamMaxResults: map[string]any{
					"type":    "integer",
					"minimum": 1,
				},
			},
		},
	}
}

func (t *findSymbolTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	searchPath := t.ws.Root()
	if dir := optionalStringArg(args, ParamDirPath); strings.TrimSpace(dir) != "" {
		resolved, err := t.ws.ResolvePath(dir)
		if err != nil {
			return nil, err
		}
		searchPath = resolved
	}

	symbolQuery := strings.TrimSpace(optionalStringArg(args, FindSymbolParamSymbol))

	kind, err := t.resolveKind(args, symbolQuery)
	if err != nil {
		return nil, err
	}

	maxResults := findSymbolDefaultMaxResults
	if n, ok, err := intArg(args, FindSymbolParamMaxResults); err != nil {
		return nil, err
	} else if ok && n > 0 {
		maxResults = n
	}

	runCtx, cancel := context.WithTimeout(ctx, findSymbolTimeout)
	defer cancel()

	files, scanErr := t.collectFiles(runCtx, searchPath)
	if scanErr != nil {
		return nil, scanErr
	}

	scannedFiles, tags := t.tagFiles(runCtx, files, symbolQuery, kind)
	if err := runCtx.Err(); err != nil {
		return nil, err
	}

	sortTags(tags)
	truncated := len(tags) > maxResults
	if truncated {
		tags = tags[:maxResults]
	}

	defs, refs := countKinds(tags)
	return map[string]any{
		"symbol":        symbolQuery,
		"matches":       t.renderMatches(tags),
		"count":         len(tags),
		"definitions":   defs,
		"references":    refs,
		"scanned_files": scannedFiles,
		"truncated":     truncated,
	}, nil
}

// resolveKind reads the kind parameter. When no symbol is given the tool acts as
// an outline and always returns definitions only.
func (t *findSymbolTool) resolveKind(args map[string]any, symbolQuery string) (string, error) {
	kind := findSymbolKindDefinition
	if raw := strings.TrimSpace(optionalStringArg(args, FindSymbolParamKind)); raw != "" {
		switch strings.ToLower(raw) {
		case findSymbolKindDefinition, findSymbolKindReference, findSymbolKindAll:
			kind = strings.ToLower(raw)
		default:
			return "", fmt.Errorf("parameter %q must be one of definition, reference, all", FindSymbolParamKind)
		}
	}
	if symbolQuery == "" {
		// Outline mode: references without a name filter are pure noise.
		kind = findSymbolKindDefinition
	}
	return kind, nil
}

// collectFiles returns the candidate files to parse. A single file resolves to
// itself; a directory is enumerated with ripgrep (which honors .gitignore) and
// capped at findSymbolMaxScanFiles.
func (t *findSymbolTool) collectFiles(ctx context.Context, searchPath string) ([]string, error) {
	info, err := os.Stat(searchPath)
	if err != nil {
		return nil, fmt.Errorf("find_symbol: stat path: %w", err)
	}
	if !info.IsDir() {
		return []string{searchPath}, nil
	}

	rgPath, err := exec.LookPath("rg")
	if err != nil {
		return nil, fmt.Errorf("ripgrep (rg) not found in PATH. Please ask the user to install ripgrep to use find_symbol")
	}

	cmd := exec.CommandContext(ctx, rgPath, "--files", searchPath)
	cmd.Dir = t.ws.Root()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil // no files
		}
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("find_symbol: list files: %s", strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("find_symbol: list files: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	files := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, line)
		if len(files) >= findSymbolMaxScanFiles {
			break
		}
	}
	return files, nil
}

// tagFiles parses each candidate and returns the matching tags plus the number
// of files that produced usable symbols.
func (t *findSymbolTool) tagFiles(ctx context.Context, files []string, symbolQuery, kind string) (int, []symbols.Tag) {
	lowerQuery := strings.ToLower(symbolQuery)
	var scanned int
	var out []symbols.Tag
	for _, file := range files {
		if ctx.Err() != nil {
			break
		}
		info, err := os.Stat(file)
		if err != nil || info.IsDir() || info.Size() > findSymbolMaxFileBytes {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		tags, quality, err := symbols.TagSource(file, src)
		if err != nil || quality == symbols.QualityNone {
			continue
		}
		scanned++
		rel := t.relPath(file)
		for _, tg := range tags {
			if !matchesKind(tg.Kind, kind) {
				continue
			}
			if lowerQuery != "" && !strings.Contains(strings.ToLower(tg.Name), lowerQuery) {
				continue
			}
			tg.Path = rel
			out = append(out, tg)
		}
	}
	return scanned, out
}

func (t *findSymbolTool) relPath(file string) string {
	if rel, err := filepath.Rel(t.ws.Root(), file); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return file
}

func (t *findSymbolTool) renderMatches(tags []symbols.Tag) string {
	if len(tags) == 0 {
		return "(no symbols found)"
	}
	lines := make([]string, 0, len(tags))
	for _, tg := range tags {
		category := tg.Category
		if category == "" {
			category = "symbol"
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s %s %s",
			tg.Path, tg.Line, tg.Column, tg.Kind, category, tg.Name))
	}
	return strings.Join(lines, "\n")
}

func matchesKind(tagKind, want string) bool {
	if want == findSymbolKindAll {
		return true
	}
	return tagKind == want
}

func countKinds(tags []symbols.Tag) (defs, refs int) {
	for _, tg := range tags {
		switch tg.Kind {
		case symbols.KindDefinition:
			defs++
		case symbols.KindReference:
			refs++
		}
	}
	return defs, refs
}

// sortTags orders tags deterministically by path, then line, then column, then
// name so repeated calls and cross-process runs produce identical output.
func sortTags(tags []symbols.Tag) {
	sort.Slice(tags, func(i, j int) bool {
		a, b := tags[i], tags[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Name < b.Name
	})
}
