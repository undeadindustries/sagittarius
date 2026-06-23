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
