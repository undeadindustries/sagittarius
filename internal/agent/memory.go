package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	geminiMDFilename = "GEMINI.md"
	agentsMDFilename = "AGENTS.md"
)

// DiscoverSystemInstruction loads project and global memory files for the system prompt.
// It walks upward from startDir for GEMINI.md (AGENTS.md when GEMINI.md is absent per
// directory) and prepends ~/.gemini/GEMINI.md when present.
func DiscoverSystemInstruction(startDir string) (string, error) {
	if strings.TrimSpace(startDir) == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("discover system instruction: %w", err)
		}
	}

	startDir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("discover system instruction: %w", err)
	}

	var sections []string

	globalPath, err := globalMemoryPath()
	if err != nil {
		return "", err
	}
	if content, ok := readMemoryFile(globalPath); ok {
		sections = append(sections, formatMemorySection(globalPath, content))
	}

	projectPaths, err := discoverProjectMemoryPaths(startDir)
	if err != nil {
		return "", err
	}
	for _, path := range projectPaths {
		content, ok := readMemoryFile(path)
		if !ok {
			continue
		}
		sections = append(sections, formatMemorySection(path, content))
	}

	return strings.Join(sections, "\n\n"), nil
}

func globalMemoryPath() (string, error) {
	dir, err := config.ResolveGeminiDir()
	if err != nil {
		return "", fmt.Errorf("resolve global memory dir: %w", err)
	}
	return filepath.Join(dir, geminiMDFilename), nil
}

func discoverProjectMemoryPaths(startDir string) ([]string, error) {
	geminiDir, err := config.ResolveGeminiDir()
	if err != nil {
		return nil, fmt.Errorf("resolve gemini dir: %w", err)
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

		if samePath(current, geminiDir) {
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
	geminiPath := filepath.Join(dir, geminiMDFilename)
	if _, err := os.Stat(geminiPath); err == nil {
		return geminiPath
	}
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
