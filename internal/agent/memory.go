package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const agentsMDFilename = "AGENTS.md"

// memoryFile is a discovered AGENTS.md path paired with its trimmed content.
type memoryFile struct {
	path    string
	content string
}

// DiscoverSystemInstruction loads project and global memory files for the system prompt.
// It walks upward from startDir collecting AGENTS.md files and prepends the global
// ~/.sagittarius/AGENTS.md when present.
func DiscoverSystemInstruction(startDir string) (string, error) {
	files, err := discoverMemoryFiles(startDir)
	if err != nil {
		return "", err
	}
	sections := make([]string, 0, len(files))
	for _, f := range files {
		sections = append(sections, formatMemorySection(f.path, f.content))
	}
	return strings.Join(sections, "\n\n"), nil
}

// DiscoverMemoryFiles returns the ordered paths of the AGENTS.md files that
// contribute to the system instruction (global first, then project files from
// the home boundary down to startDir). Only files with non-empty content are
// included, matching what DiscoverSystemInstruction loads. It is used to tell
// the user which memory files were loaded.
func DiscoverMemoryFiles(startDir string) ([]string, error) {
	files, err := discoverMemoryFiles(startDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.path)
	}
	return paths, nil
}

func discoverMemoryFiles(startDir string) ([]memoryFile, error) {
	if strings.TrimSpace(startDir) == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("discover system instruction: %w", err)
		}
	}

	startDir, err := filepath.Abs(startDir)
	if err != nil {
		return nil, fmt.Errorf("discover system instruction: %w", err)
	}

	var files []memoryFile

	globalPath, err := globalMemoryPath()
	if err != nil {
		return nil, err
	}
	if content, ok := readMemoryFile(globalPath); ok {
		files = append(files, memoryFile{path: globalPath, content: content})
	}

	projectPaths, err := discoverProjectMemoryPaths(startDir)
	if err != nil {
		return nil, err
	}
	for _, path := range projectPaths {
		content, ok := readMemoryFile(path)
		if !ok {
			continue
		}
		files = append(files, memoryFile{path: path, content: content})
	}

	return files, nil
}

func globalMemoryPath() (string, error) {
	path, err := config.ResolveGlobalAgentsPath()
	if err != nil {
		return "", fmt.Errorf("resolve global memory dir: %w", err)
	}
	return path, nil
}

func discoverProjectMemoryPaths(startDir string) ([]string, error) {
	homeDir, err := config.ResolveSagittariusDir()
	if err != nil {
		return nil, fmt.Errorf("resolve sagittarius dir: %w", err)
	}

	var paths []string
	seen := make(map[string]struct{})
	current := startDir

	for {
		if path := memoryFileInDir(current); path != "" {
			if _, ok := seen[path]; !ok {
				seen[path] = struct{}{}
				paths = append([]string{path}, paths...)
			}
		}

		if samePath(current, homeDir) {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	return paths, nil
}

func memoryFileInDir(dir string) string {
	agentsPath := filepath.Join(dir, agentsMDFilename)
	if _, err := os.Stat(agentsPath); err == nil {
		return agentsPath
	}
	return ""
}

func readMemoryFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", false
	}
	return content, true
}

func formatMemorySection(path, content string) string {
	return fmt.Sprintf("# Context from %s\n\n%s", path, content)
}

func samePath(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return absA == absB
}

// --- Managed memory section: /memory add|list|remove and the save_memory tool ---
//
// Added memories live under one heading appended to the target AGENTS.md.
// Everything outside that section, including its own hand-written content, is
// preserved untouched; only the blank-line spacing immediately at the
// section's seams is normalized on rewrite.

// memorySectionHeading marks the block of AGENTS.md owned by /memory
// add/remove and the save_memory tool.
const memorySectionHeading = "## Sagittarius Added Memories"

// MemoryEntry is one managed-section bullet paired with the scope and file it
// came from, for /memory list.
type MemoryEntry struct {
	Scope config.SettingScope
	Path  string
	Text  string
}

// MemoryFilePath resolves the AGENTS.md file /memory and save_memory write to
// for the given scope: the global ~/.sagittarius/AGENTS.md, or the project's
// <workDir>/AGENTS.md (the same file /init populates).
func MemoryFilePath(scope config.SettingScope, workDir string) (string, error) {
	if scope == config.ScopeProject {
		if strings.TrimSpace(workDir) == "" {
			return "", fmt.Errorf("memory: work directory is required for project scope")
		}
		return filepath.Join(workDir, agentsMDFilename), nil
	}
	return globalMemoryPath()
}

// AddMemory sanitizes and appends text as a new bullet in scope's managed
// memory section, creating the file and its parent directory if needed. It
// returns the resolved file path.
func AddMemory(scope config.SettingScope, workDir, text string) (string, error) {
	clean := sanitizeMemoryText(text)
	if clean == "" {
		return "", fmt.Errorf("memory text must not be empty")
	}
	path, err := MemoryFilePath(scope, workDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("memory: create %s: %w", filepath.Dir(path), err)
	}
	original, err := readFileIfExists(path)
	if err != nil {
		return "", err
	}
	lines := splitLines(original)
	entries, sectionStart, sectionEnd := parseMemoryLines(lines)
	entries = append(entries, clean)
	if err := writeFileAtomic(path, renderMemoryFile(lines, sectionStart, sectionEnd, entries)); err != nil {
		return "", err
	}
	return path, nil
}

// ListMemories returns every managed-section entry across both scopes,
// global first then project, in the order /memory list numbers them and
// RemoveMemory indexes them.
func ListMemories(workDir string) ([]MemoryEntry, error) {
	var out []MemoryEntry
	for _, scope := range []config.SettingScope{config.ScopeGlobal, config.ScopeProject} {
		path, err := MemoryFilePath(scope, workDir)
		if err != nil {
			return nil, err
		}
		content, err := readFileIfExists(path)
		if err != nil {
			return nil, err
		}
		entries, _, _ := parseMemoryLines(splitLines(content))
		for _, text := range entries {
			out = append(out, MemoryEntry{Scope: scope, Path: path, Text: text})
		}
	}
	return out, nil
}

// RemoveMemory deletes the 1-based index-th entry in ListMemories order,
// re-reading both files fresh so a hand-edit between /memory list and
// /memory remove is respected rather than acted on stale data. It returns the
// removed text; removing a scope's only remaining entry also removes that
// file's now-empty heading.
func RemoveMemory(workDir string, index int) (string, error) {
	if index < 1 {
		return "", fmt.Errorf("memory index must be 1 or greater")
	}
	total := 0
	for _, scope := range []config.SettingScope{config.ScopeGlobal, config.ScopeProject} {
		path, err := MemoryFilePath(scope, workDir)
		if err != nil {
			return "", err
		}
		content, err := readFileIfExists(path)
		if err != nil {
			return "", err
		}
		lines := splitLines(content)
		entries, sectionStart, sectionEnd := parseMemoryLines(lines)
		if index > total+len(entries) {
			total += len(entries)
			continue
		}
		localIdx := index - total - 1
		removed := entries[localIdx]
		entries = append(entries[:localIdx:localIdx], entries[localIdx+1:]...)
		if err := writeFileAtomic(path, renderMemoryFile(lines, sectionStart, sectionEnd, entries)); err != nil {
			return "", err
		}
		return removed, nil
	}
	if total == 0 {
		return "", fmt.Errorf("no memory entries found")
	}
	return "", fmt.Errorf("memory index %d out of range (1-%d)", index, total)
}

// sanitizeMemoryText collapses internal whitespace (including newlines) to
// single spaces and strips leading markdown bullet/heading/quote punctuation,
// so a memory entry can never forge a second bullet, a fake heading, or a
// duplicate managed-section header.
func sanitizeMemoryText(text string) string {
	joined := strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(strings.TrimLeft(joined, "-*#> "))
}

// splitLines splits content into lines, dropping the trailing empty element a
// final "\n" would otherwise produce. Empty content yields nil.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// parseMemoryLines locates the managed section inside lines and returns its
// entries plus the [start,end) range it occupies. Both bounds equal
// len(lines) when no section is present, so a new one is appended at end of
// file. The section runs from the heading line to the next markdown heading
// line or end of file; blank lines within it are skipped.
func parseMemoryLines(lines []string) (entries []string, sectionStart, sectionEnd int) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == memorySectionHeading {
			start = i
			break
		}
	}
	if start == -1 {
		return nil, len(lines), len(lines)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "#") {
			end = i
			break
		}
	}
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		entries = append(entries, stripBulletPrefix(trimmed))
	}
	return entries, start, end
}

func stripBulletPrefix(line string) string {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			return rest
		}
	}
	return line
}

// renderMemoryFile rebuilds content, replacing the [sectionStart,sectionEnd)
// line range with a managed section holding entries (or removing the section
// entirely when entries is empty). Lines outside that range are never
// altered; only the blank-line spacing immediately at its seams is
// normalized.
func renderMemoryFile(lines []string, sectionStart, sectionEnd int, entries []string) string {
	before := trimTrailingBlank(lines[:sectionStart])
	after := trimLeadingBlank(lines[sectionEnd:])

	var out []string
	out = append(out, before...)
	if len(entries) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, memorySectionHeading, "")
		for _, e := range entries {
			out = append(out, "- "+e)
		}
	}
	if len(after) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, after...)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func trimTrailingBlank(lines []string) []string {
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[:end]
}

func trimLeadingBlank(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	return lines[start:]
}

// readFileIfExists returns "" (no error) when path does not exist, unlike
// os.ReadFile.
func readFileIfExists(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(data), nil
}

// writeFileAtomic writes content to path via a same-directory temp file plus
// rename, so a crash mid-write cannot truncate an existing AGENTS.md.
func writeFileAtomic(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".agents-md-*.tmp")
	if err != nil {
		return fmt.Errorf("memory: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }() // no-op once renamed away
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("memory: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("memory: close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("memory: rename temp file: %w", err)
	}
	return nil
}
