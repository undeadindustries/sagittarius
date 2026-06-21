package session

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/storage"
)

// ProjectHash returns the SHA-256 hex digest of the absolute project root path.
// It is stored in the session metadata's projectHash field as a stable content
// identifier; the on-disk directory layout keys off the project slug instead.
func ProjectHash(projectRoot string) string {
	sum := sha256.Sum256([]byte(projectRoot))
	return hex.EncodeToString(sum[:])
}

// ChatsDir returns the chats directory for a project root.
// Format: ~/.sagittarius/tmp/<slug>/chats/ (slug via the project registry).
func ChatsDir(projectRoot string) (string, error) {
	tmpDir, err := storage.ProjectTmpDir(projectRoot)
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, "chats"), nil
}
