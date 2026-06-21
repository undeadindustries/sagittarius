package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ProjectHash returns the SHA-256 hex digest of the absolute project root path.
// This matches the fork's getProjectHash() in packages/core/src/utils/paths.ts,
// enabling cross-tool session compatibility.
func ProjectHash(projectRoot string) string {
	sum := sha256.Sum256([]byte(projectRoot))
	return hex.EncodeToString(sum[:])
}

// ChatsDir returns the path to the chats directory for a given project root.
// Format: ~/.gemini/tmp/<project_hash>/chats/
func ChatsDir(projectRoot string) (string, error) {
	home, err := geminiHome()
	if err != nil {
		return "", err
	}
	hash := ProjectHash(projectRoot)
	return filepath.Join(home, "tmp", hash, "chats"), nil
}

// geminiHome returns the base directory for Sagittarius/gemini-cli data.
// Respects GEMINI_CLI_HOME env var (fork-compatible).
func geminiHome() (string, error) {
	if v := os.Getenv("GEMINI_CLI_HOME"); v != "" {
		return filepath.Join(v, ".gemini"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".gemini"), nil
}
