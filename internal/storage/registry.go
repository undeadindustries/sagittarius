package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	projectsFileName = "projects.json"
	projectRootFile  = ".project_root"

	lockTimeout    = 10 * time.Second
	lockRetryDelay = 100 * time.Millisecond
	lockStaleAfter = 30 * time.Second
)

var slugStripRe = regexp.MustCompile(`[^a-z0-9]+`)

// registryData is the on-disk projects.json shape: absolute path -> slug.
type registryData struct {
	Projects map[string]string `json:"projects"`
}

// projectRegistry maps absolute project paths to short slugs and owns the
// tmp/<slug> and history/<slug> ownership markers. Port of the fork
// ProjectRegistry (packages/core/src/config/projectRegistry.ts).
type projectRegistry struct {
	registryPath string
	baseDirs     []string
}

func newProjectRegistry() (*projectRegistry, error) {
	dir, err := config.ResolveSagittariusDir()
	if err != nil {
		return nil, fmt.Errorf("resolve sagittarius dir: %w", err)
	}
	tmp, err := TmpDir()
	if err != nil {
		return nil, err
	}
	history, err := HistoryDir()
	if err != nil {
		return nil, err
	}
	return &projectRegistry{
		registryPath: filepath.Join(dir, projectsFileName),
		baseDirs:     []string{tmp, history},
	}, nil
}

// shortID returns the slug for projectPath, generating and persisting one on
// first use. The update is guarded by a lock file for cross-process safety.
func (r *projectRegistry) shortID(projectPath string) (string, error) {
	normalized := r.normalizePath(projectPath)
	if err := os.MkdirAll(filepath.Dir(r.registryPath), 0o700); err != nil {
		return "", fmt.Errorf("create registry dir: %w", err)
	}

	release, err := r.acquireLock()
	if err != nil {
		return "", err
	}
	defer release()

	data := r.load()

	if slug, ok := data.Projects[normalized]; ok {
		if r.verifySlugOwnership(slug, normalized) {
			if err := r.ensureOwnershipMarkers(slug, normalized); err != nil {
				return "", err
			}
			return slug, nil
		}
		delete(data.Projects, normalized)
	}

	slug := r.findExistingSlugForPath(normalized)
	if slug == "" {
		slug, err = r.claimNewSlug(normalized, data.Projects)
		if err != nil {
			return "", err
		}
	}

	data.Projects[normalized] = slug
	if err := r.save(data); err != nil {
		return "", err
	}
	return slug, nil
}

func (r *projectRegistry) load() registryData {
	raw, err := os.ReadFile(r.registryPath)
	if err != nil {
		return registryData{Projects: map[string]string{}}
	}
	var data registryData
	if err := json.Unmarshal(raw, &data); err != nil || data.Projects == nil {
		return registryData{Projects: map[string]string{}}
	}
	return data
}

func (r *projectRegistry) save(data registryData) error {
	if data.Projects == nil {
		data.Projects = map[string]string{}
	}
	out, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project registry: %w", err)
	}
	tmp := r.registryPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("write project registry: %w", err)
	}
	if err := os.Rename(tmp, r.registryPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename project registry: %w", err)
	}
	return nil
}

func (r *projectRegistry) verifySlugOwnership(slug, projectPath string) bool {
	for _, baseDir := range r.baseDirs {
		marker := filepath.Join(baseDir, slug, projectRootFile)
		raw, err := os.ReadFile(marker)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return false
		}
		if r.normalizePath(strings.TrimSpace(string(raw))) != projectPath {
			return false
		}
	}
	return true
}

func (r *projectRegistry) findExistingSlugForPath(projectPath string) string {
	for _, baseDir := range r.baseDirs {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			marker := filepath.Join(baseDir, entry.Name(), projectRootFile)
			raw, err := os.ReadFile(marker)
			if err != nil {
				continue
			}
			if r.normalizePath(strings.TrimSpace(string(raw))) == projectPath {
				if err := r.ensureOwnershipMarkers(entry.Name(), projectPath); err == nil {
					return entry.Name()
				}
			}
		}
	}
	return ""
}

func (r *projectRegistry) claimNewSlug(projectPath string, existing map[string]string) (string, error) {
	base := r.slugify(filepath.Base(projectPath))
	taken := make(map[string]struct{}, len(existing))
	for _, v := range existing {
		taken[v] = struct{}{}
	}

	for counter := 0; ; counter++ {
		candidate := base
		if counter > 0 {
			candidate = fmt.Sprintf("%s-%d", base, counter)
		}
		if _, ok := taken[candidate]; ok {
			continue
		}
		if r.diskCollision(candidate, projectPath) {
			continue
		}
		if err := r.ensureOwnershipMarkers(candidate, projectPath); err != nil {
			if isCollision(err) {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
}

func (r *projectRegistry) diskCollision(slug, projectPath string) bool {
	for _, baseDir := range r.baseDirs {
		marker := filepath.Join(baseDir, slug, projectRootFile)
		raw, err := os.ReadFile(marker)
		if err != nil {
			continue
		}
		if r.normalizePath(strings.TrimSpace(string(raw))) != projectPath {
			return true
		}
	}
	return false
}

func (r *projectRegistry) ensureOwnershipMarkers(slug, projectPath string) error {
	for _, baseDir := range r.baseDirs {
		slugDir := filepath.Join(baseDir, slug)
		if err := os.MkdirAll(slugDir, 0o700); err != nil {
			return fmt.Errorf("create slug dir %q: %w", slugDir, err)
		}
		marker := filepath.Join(slugDir, projectRootFile)
		raw, err := os.ReadFile(marker)
		if err == nil {
			if r.normalizePath(strings.TrimSpace(string(raw))) == projectPath {
				continue
			}
			return &collisionError{slug: slug, owner: strings.TrimSpace(string(raw))}
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read ownership marker %q: %w", marker, err)
		}
		if err := os.WriteFile(marker, []byte(projectPath), 0o600); err != nil {
			return fmt.Errorf("write ownership marker %q: %w", marker, err)
		}
	}
	return nil
}

func (r *projectRegistry) normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	if runtime.GOOS == "windows" {
		abs = strings.ToLower(abs)
	}
	return abs
}

func (r *projectRegistry) slugify(text string) string {
	s := slugStripRe.ReplaceAllString(strings.ToLower(text), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "project"
	}
	return s
}

// acquireLock takes an exclusive lock file next to the registry, breaking a
// stale lock older than lockStaleAfter. The returned func releases the lock.
func (r *projectRegistry) acquireLock() (func(), error) {
	lockPath := r.registryPath + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire registry lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil {
			if time.Since(info.ModTime()) > lockStaleAfter {
				_ = os.Remove(lockPath)
				continue
			}
		}
		if time.Now().After(deadline) {
			// Proceed without the lock rather than blocking startup forever;
			// writes are atomic renames, so the worst case is a benign reslug.
			return func() {}, nil
		}
		time.Sleep(lockRetryDelay)
	}
}

type collisionError struct {
	slug  string
	owner string
}

func (e *collisionError) Error() string {
	return fmt.Sprintf("slug %q already owned by %q", e.slug, e.owner)
}

func isCollision(err error) bool {
	var c *collisionError
	return errors.As(err, &c)
}
