package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	tmpDirName     = "tmp"
	historyDirName = "history"
)

// EnsureGlobalHome creates ~/.sagittarius (or $SAGITTARIUS_HOME/.sagittarius)
// with 0700 permissions when it does not yet exist. It is idempotent and writes
// no files beyond the directory itself.
func EnsureGlobalHome() (string, error) {
	dir, err := config.ResolveSagittariusDir()
	if err != nil {
		return "", fmt.Errorf("resolve sagittarius dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create sagittarius home %q: %w", dir, err)
	}
	return dir, nil
}

// TmpDir returns ~/.sagittarius/tmp.
func TmpDir() (string, error) {
	dir, err := config.ResolveSagittariusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tmpDirName), nil
}

// HistoryDir returns ~/.sagittarius/history.
func HistoryDir() (string, error) {
	dir, err := config.ResolveSagittariusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, historyDirName), nil
}

// ProjectSlug resolves the stable short identifier for projectRoot, creating
// projects.json and the tmp/history ownership markers on first use.
func ProjectSlug(projectRoot string) (string, error) {
	reg, err := newProjectRegistry()
	if err != nil {
		return "", err
	}
	return reg.shortID(projectRoot)
}

// ProjectTmpDir returns ~/.sagittarius/tmp/<slug> for projectRoot.
func ProjectTmpDir(projectRoot string) (string, error) {
	slug, err := ProjectSlug(projectRoot)
	if err != nil {
		return "", err
	}
	base, err := TmpDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, slug), nil
}
