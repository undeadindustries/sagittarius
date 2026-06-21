package skills

import (
	"os"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// UserSkillsDir returns ~/.gemini/skills.
func UserSkillsDir() (string, error) {
	dir, err := config.ResolveGeminiDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skills"), nil
}

// UserAgentSkillsDir returns ~/.agents/skills.
func UserAgentSkillsDir() (string, error) {
	home, err := config.ResolveHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agents", "skills"), nil
}

// ProjectSkillsDir returns <workDir>/.gemini/skills.
func ProjectSkillsDir(workDir string) string {
	return filepath.Join(workDir, config.GeminiDir, "skills")
}

// ProjectAgentSkillsDir returns <workDir>/.agents/skills.
func ProjectAgentSkillsDir(workDir string) string {
	return filepath.Join(workDir, ".agents", "skills")
}

// DirExists reports whether path is an existing directory.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
