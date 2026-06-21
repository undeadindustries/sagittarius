package skills

import (
	"os"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// UserSkillsDir returns ~/.sagittarius/skills.
func UserSkillsDir() (string, error) {
	dir, err := config.ResolveSagittariusDir()
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

// ProjectSkillsDir returns <workDir>/.sagittarius/skills.
func ProjectSkillsDir(workDir string) string {
	return filepath.Join(workDir, config.SagittariusDir, "skills")
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
