// Package customprompt stores user-authored system prompts as one Markdown
// file per prompt under ~/.sagittarius/prompts/. Prompts are global only: a
// cloned repository must not be able to ship text that becomes the agent's
// system prompt, the same reason provider definitions and keys never come from
// project settings.
//
// The package is a leaf. It reads and writes files and validates names and
// bodies; selecting a prompt and building the system instruction happen in
// internal/config, internal/prompt, and internal/agent.
package customprompt

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const (
	// DirName is the prompts directory under the Sagittarius home.
	DirName = "prompts"
	// FileExt is the extension of every stored prompt file.
	FileExt = ".md"
	// MaxNameLen bounds a prompt name so it fits picker rows and file names.
	MaxNameLen = 48
	// MaxPromptBytes caps a prompt body. The system prompt is resent on every
	// request and is the provider's cache prefix, so an accidental paste of a
	// large file would be billed on every turn.
	MaxPromptBytes = 64 << 10

	dirPerm  = 0o700
	filePerm = 0o600
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

var (
	// ErrNotFound is returned when a named prompt has no file.
	ErrNotFound = errors.New("custom prompt not found")
	// ErrExists is returned when a create or rename would replace another prompt.
	ErrExists = errors.New("custom prompt already exists")
	// ErrInvalidName is returned for names that fail NormalizeName.
	ErrInvalidName = errors.New("invalid custom prompt name")
	// ErrInvalidBody is returned for empty, oversized, or non-text bodies.
	ErrInvalidBody = errors.New("invalid custom prompt text")
)

// Entry describes one stored prompt.
type Entry struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
}

// Store reads and writes prompt files in a single directory.
type Store struct {
	dir string
}

// NewStore returns a store rooted at dir. Tests pass a temp directory.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// DefaultStore returns the store under the resolved Sagittarius home, which
// honors SAGITTARIUS_HOME.
func DefaultStore() (*Store, error) {
	home, err := config.ResolveSagittariusDir()
	if err != nil {
		return nil, fmt.Errorf("resolve sagittarius dir: %w", err)
	}
	return NewStore(filepath.Join(home, DirName)), nil
}

// Dir returns the directory the store reads and writes.
func (s *Store) Dir() string { return s.dir }

// NormalizeName lowercases name, turns spaces and underscores into hyphens,
// and validates the result. Names never equal a built-in preset id, so a
// stored selection always means exactly one prompt.
func NormalizeName(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.Join(strings.FieldsFunc(n, func(r rune) bool { return r == ' ' || r == '_' || r == '\t' }), "-")
	switch {
	case n == "":
		return "", fmt.Errorf("%w: name is empty", ErrInvalidName)
	case len(n) > MaxNameLen:
		return "", fmt.Errorf("%w: %q is longer than %d characters", ErrInvalidName, n, MaxNameLen)
	case !namePattern.MatchString(n):
		return "", fmt.Errorf("%w: %q may contain only lowercase letters, digits, and hyphens, and must start with a letter or digit", ErrInvalidName, n)
	}
	if _, ok := config.LookupPreset(n); ok {
		return "", fmt.Errorf("%w: %q is a built-in system prompt", ErrInvalidName, n)
	}
	return n, nil
}

// ValidateBody checks that body is usable as a system prompt and returns it
// with surrounding whitespace trimmed.
func ValidateBody(body string) (string, error) {
	if len(body) > MaxPromptBytes {
		return "", fmt.Errorf("%w: %d bytes exceeds the %d byte limit", ErrInvalidBody, len(body), MaxPromptBytes)
	}
	if !utf8.ValidString(body) {
		return "", fmt.Errorf("%w: not valid UTF-8 text", ErrInvalidBody)
	}
	if strings.ContainsRune(body, 0) {
		return "", fmt.Errorf("%w: contains a NUL byte", ErrInvalidBody)
	}
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "", fmt.Errorf("%w: text is empty", ErrInvalidBody)
	}
	return trimmed, nil
}

// List returns every stored prompt sorted by name. A missing directory is an
// empty list, not an error.
func (s *Store) List() ([]Entry, error) {
	dirents, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list custom prompts in %s: %w", s.dir, err)
	}
	out := make([]Entry, 0, len(dirents))
	for _, d := range dirents {
		if d.IsDir() || !strings.HasSuffix(d.Name(), FileExt) {
			continue
		}
		name := strings.TrimSuffix(d.Name(), FileExt)
		if n, err := NormalizeName(name); err != nil || n != name {
			continue
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		out = append(out, Entry{Name: name, Path: filepath.Join(s.dir, d.Name()), Size: info.Size(), ModTime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Exists reports whether a prompt with name is stored.
func (s *Store) Exists(name string) bool {
	n, err := NormalizeName(name)
	if err != nil {
		return false
	}
	info, err := os.Stat(s.path(n))
	return err == nil && info.Mode().IsRegular()
}

// Load returns the validated text of the named prompt.
func (s *Store) Load(name string) (string, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	body, err := readCapped(s.path(n))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %q", ErrNotFound, n)
	}
	if err != nil {
		return "", fmt.Errorf("load custom prompt %q: %w", n, err)
	}
	return ValidateBody(body)
}

// Save creates or replaces the named prompt. The write goes through a temp
// file and rename so a crash or full disk never leaves a half-written prompt.
func (s *Store) Save(name, body string) (string, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	text, err := ValidateBody(body)
	if err != nil {
		return "", err
	}
	if err := s.writeAtomic(n, text+"\n"); err != nil {
		return "", err
	}
	return n, nil
}

// Create is Save that refuses to replace an existing prompt.
func (s *Store) Create(name, body string) (string, error) {
	n, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	if s.Exists(n) {
		return "", fmt.Errorf("%w: %q", ErrExists, n)
	}
	return s.Save(n, body)
}

// ReadImportFile reads a prompt body from srcPath for import. A leading "~"
// expands to the user's home directory; relative paths resolve against
// workDir. The file must be a regular file within MaxPromptBytes. The caller
// stores a copy, so the source is never referenced again.
func ReadImportFile(srcPath, workDir string) (string, error) {
	p := strings.TrimSpace(srcPath)
	if p == "" {
		return "", errors.New("import path is empty")
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand ~ in %q: %w", srcPath, err)
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(workDir, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("import %s: %w", p, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("import %s: not a regular file", p)
	}
	if info.Size() > MaxPromptBytes {
		return "", fmt.Errorf("%w: %s is %d bytes, over the %d byte limit", ErrInvalidBody, p, info.Size(), MaxPromptBytes)
	}
	body, err := readCapped(p)
	if err != nil {
		return "", fmt.Errorf("import %s: %w", p, err)
	}
	return ValidateBody(body)
}

// Rename moves a prompt to a new name. It fails if newName is already stored.
func (s *Store) Rename(oldName, newName string) (string, error) {
	from, err := NormalizeName(oldName)
	if err != nil {
		return "", err
	}
	to, err := NormalizeName(newName)
	if err != nil {
		return "", err
	}
	if from == to {
		return to, nil
	}
	if !s.Exists(from) {
		return "", fmt.Errorf("%w: %q", ErrNotFound, from)
	}
	if s.Exists(to) {
		return "", fmt.Errorf("%w: %q", ErrExists, to)
	}
	if err := os.Rename(s.path(from), s.path(to)); err != nil {
		return "", fmt.Errorf("rename custom prompt %q to %q: %w", from, to, err)
	}
	return to, nil
}

// Delete removes the named prompt file.
func (s *Store) Delete(name string) error {
	n, err := NormalizeName(name)
	if err != nil {
		return err
	}
	if err := os.Remove(s.path(n)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %q", ErrNotFound, n)
		}
		return fmt.Errorf("delete custom prompt %q: %w", n, err)
	}
	return nil
}

func (s *Store) path(name string) string {
	return filepath.Join(s.dir, name+FileExt)
}

func (s *Store) writeAtomic(name, text string) error {
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("create %s: %w", s.dir, err)
	}
	tmp, err := os.CreateTemp(s.dir, "."+name+"-*.tmp")
	if err != nil {
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		// Best effort: the temp file is never referenced again.
		_ = os.Remove(tmpPath)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	if _, err := tmp.WriteString(text); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	if err := os.Rename(tmpPath, s.path(name)); err != nil {
		cleanup()
		return fmt.Errorf("save custom prompt %q: %w", name, err)
	}
	return nil
}

// readCapped reads at most MaxPromptBytes+1 bytes so an oversized file is
// reported by ValidateBody instead of being loaded whole.
func readCapped(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, MaxPromptBytes+1))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
