// Package snapshot records the file mutations Sagittarius performs during a
// session so the user can review them (/diff) and revert them (/undo).
//
// Rather than a shadow git repository (the OpenCode/fork approach, which has
// well-documented data-loss failure modes around silent `git add` failures and
// multi-gigabyte full-body diffs), Sagittarius captures the before/after
// content of each write_file directly. This is dependency-free, works in
// non-git projects, is session-scoped by construction (only Sagittarius writes
// are tracked, never unrelated worktree drift), and computes unified diffs in
// process. Per-file content is capped (config.DefaultSnapshotMaxFileBytes) so a
// single huge file is recorded as metadata only.
//
// The snapshot index lives under ~/.sagittarius/tmp/<slug>/snapshots/, outside
// the project root, so the agent's workspace-bounded file tools can never reach
// or corrupt it.
package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/storage"
)

// Options configures a Manager.
type Options struct {
	// MaxFileBytes caps the size of file content captured per snapshot. Files
	// larger than this are recorded as metadata only (diff/undo unavailable).
	// Non-positive falls back to config.DefaultSnapshotMaxFileBytes.
	MaxFileBytes int
}

// change records one write_file mutation.
type change struct {
	Rel     string    `json:"path"`
	Tool    string    `json:"tool"`
	Before  string    `json:"before"`
	After   string    `json:"after"`
	Existed bool      `json:"existed"`
	Skipped bool      `json:"skipped,omitempty"`
	Seq     int       `json:"seq"`
	Time    time.Time `json:"time"`
}

// Manager tracks per-session file changes.
type Manager struct {
	root         string
	indexPath    string
	maxFileBytes int

	mu      sync.Mutex
	pending map[string]captured // absPath -> state captured before the write
	stack   []change            // LIFO undo stack for this session

	firstBefore  map[string]string // rel -> content before the first write
	firstExisted map[string]bool   // rel -> existed before the first write
	lastAfter    map[string]string // rel -> content after the latest write
	seq          int
}

type captured struct {
	content string
	existed bool
	skipped bool
}

// NewManager constructs a Manager rooted at projectRoot. The session index is
// created under ~/.sagittarius/tmp/<slug>/snapshots/<sessionID>.jsonl.
func NewManager(projectRoot, sessionID string, opts Options) (*Manager, error) {
	root, err := canonicalRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	tmp, err := storage.ProjectTmpDir(root)
	if err != nil {
		return nil, fmt.Errorf("snapshot: resolve tmp dir: %w", err)
	}
	snapDir := filepath.Join(tmp, "snapshots")
	if err := os.MkdirAll(snapDir, 0o700); err != nil {
		return nil, fmt.Errorf("snapshot: create dir %q: %w", snapDir, err)
	}
	max := opts.MaxFileBytes
	if max <= 0 {
		max = config.DefaultSnapshotMaxFileBytes
	}
	return &Manager{
		root:         root,
		indexPath:    filepath.Join(snapDir, sanitizeSessionID(sessionID)+".jsonl"),
		maxFileBytes: max,
		pending:      make(map[string]captured),
		firstBefore:  make(map[string]string),
		firstExisted: make(map[string]bool),
		lastAfter:    make(map[string]string),
	}, nil
}

// Enabled reports whether snapshotting is active. A nil Manager is disabled.
func (m *Manager) Enabled() bool { return m != nil }

// IndexPath returns the on-disk session index location (for diagnostics/docs).
func (m *Manager) IndexPath() string {
	if m == nil {
		return ""
	}
	return m.indexPath
}

// CaptureWrite records the current state of absPath immediately before a
// write_file execution. Paired with CommitWrite after the write succeeds.
func (m *Manager) CaptureWrite(absPath string) {
	if m == nil {
		return
	}
	rel, ok := m.relPath(absPath)
	if !ok {
		return
	}
	content, existed, skipped := m.readCapped(absPath)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[absPath] = captured{content: content, existed: existed, skipped: skipped}
	if _, seen := m.firstBefore[rel]; !seen {
		m.firstBefore[rel] = content
		m.firstExisted[rel] = existed
	}
}

// CommitWrite finalizes a change after a successful write_file. It reads the new
// bytes, pushes an undo entry, and appends to the on-disk session index.
func (m *Manager) CommitWrite(absPath, toolName string) {
	if m == nil {
		return
	}
	rel, ok := m.relPath(absPath)
	if !ok {
		return
	}
	after, _, afterSkipped := m.readCapped(absPath)

	m.mu.Lock()
	before := m.pending[absPath]
	delete(m.pending, absPath)
	m.seq++
	c := change{
		Rel:     rel,
		Tool:    toolName,
		Before:  before.content,
		After:   after,
		Existed: before.existed,
		Skipped: before.skipped || afterSkipped,
		Seq:     m.seq,
		Time:    time.Now().UTC(),
	}
	m.stack = append(m.stack, c)
	m.lastAfter[rel] = after
	m.mu.Unlock()

	m.appendIndex(c)
}

// Diff renders the net unified diff for files changed this session. A non-empty
// pathFilter keeps only files whose relative path contains the filter.
func (m *Manager) Diff(pathFilter string) (string, error) {
	if m == nil {
		return "", nil
	}
	m.mu.Lock()
	rels := make([]string, 0, len(m.firstBefore))
	for rel := range m.firstBefore {
		rels = append(rels, rel)
	}
	type pair struct{ before, after string }
	state := make(map[string]pair, len(rels))
	for _, rel := range rels {
		state[rel] = pair{before: m.firstBefore[rel], after: m.lastAfter[rel]}
	}
	skipped := map[string]bool{}
	for _, c := range m.stack {
		if c.Skipped {
			skipped[c.Rel] = true
		}
	}
	m.mu.Unlock()

	sort.Strings(rels)
	filter := strings.TrimSpace(pathFilter)
	var out strings.Builder
	for _, rel := range rels {
		if filter != "" && !strings.Contains(rel, filter) {
			continue
		}
		st := state[rel]
		if skipped[rel] {
			fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n@@ file too large to snapshot; diff unavailable @@\n", rel, rel)
			continue
		}
		d := UnifiedDiff(st.before, st.after, rel)
		if d == "" {
			continue
		}
		out.WriteString(d)
	}
	return out.String(), nil
}

// ChangedFiles returns the sorted relative paths with a net change this session.
func (m *Manager) ChangedFiles() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var rels []string
	for rel, before := range m.firstBefore {
		if m.lastAfter[rel] != before {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)
	return rels
}

// Undo reverts the last n recorded changes (LIFO), restoring each file's prior
// bytes (or removing files that did not exist before). It returns the relative
// paths restored. Oversized (metadata-only) changes cannot be reverted and are
// reported as an error while still restoring the rest.
func (m *Manager) Undo(n int) ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("snapshots are disabled")
	}
	if n <= 0 {
		n = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.stack) == 0 {
		return nil, fmt.Errorf("nothing to undo")
	}

	var restored []string
	var failures []string
	for i := 0; i < n && len(m.stack) > 0; i++ {
		c := m.stack[len(m.stack)-1]
		if c.Skipped {
			failures = append(failures, c.Rel+" (too large to snapshot)")
			m.stack = m.stack[:len(m.stack)-1]
			m.recomputeTracking(c.Rel)
			continue
		}
		if err := m.restore(c); err != nil {
			// Transient failure (permissions, disk full, ...). Leave the change
			// on the stack so the user can retry /undo after fixing the cause,
			// and stop here: the stack is LIFO, so older entries sit underneath
			// this one and must not be reverted ahead of it.
			failures = append(failures, fmt.Sprintf("%s (%v)", c.Rel, err))
			break
		}
		m.stack = m.stack[:len(m.stack)-1]
		m.recomputeTracking(c.Rel)
		restored = append(restored, c.Rel)
	}

	if len(failures) > 0 {
		return restored, fmt.Errorf("could not revert: %s", strings.Join(failures, "; "))
	}
	return restored, nil
}

// restore writes a change's prior content back to disk, or removes the file
// when it did not exist before the change.
func (m *Manager) restore(c change) error {
	abs := filepath.Join(m.root, filepath.FromSlash(c.Rel))
	if !c.Existed {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(c.Before), 0o644)
}

// recomputeTracking refreshes the per-file diff state after an undo: it points
// lastAfter at the now-topmost change for rel, or clears tracking when no
// change for rel remains on the stack. Caller holds m.mu.
func (m *Manager) recomputeTracking(rel string) {
	for i := len(m.stack) - 1; i >= 0; i-- {
		if m.stack[i].Rel == rel {
			m.lastAfter[rel] = m.stack[i].After
			return
		}
	}
	delete(m.firstBefore, rel)
	delete(m.firstExisted, rel)
	delete(m.lastAfter, rel)
}

// readCapped reads absPath, reporting whether it existed and whether it was
// skipped for exceeding the size cap. Skipped/missing files yield "".
func (m *Manager) readCapped(absPath string) (content string, existed bool, skipped bool) {
	info, err := os.Stat(absPath)
	if err != nil {
		return "", false, false
	}
	if info.IsDir() {
		return "", true, true
	}
	if info.Size() > int64(m.maxFileBytes) {
		return "", true, true
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", true, false
	}
	return string(data), true, false
}

func (m *Manager) relPath(absPath string) (string, bool) {
	rel, err := filepath.Rel(m.root, absPath)
	if err != nil {
		return "", false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// appendIndex appends one change record to the session JSONL index. A write
// failure is non-fatal (the in-memory stack still powers /diff and /undo) but
// is surfaced on stderr-style logging by the caller chain; here we silently
// best-effort to avoid coupling to a logger.
func (m *Manager) appendIndex(c change) {
	f, err := os.OpenFile(m.indexPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(c)
}

// canonicalRoot resolves projectRoot to an absolute, symlink-evaluated path,
// matching tools.NewWorkspace. The scheduler captures write targets via
// Workspace.ResolvePath (which returns EvalSymlinks'd paths), so the snapshot
// root must be canonicalized the same way or relPath rejects every capture when
// the working directory is reached through a symlink.
func canonicalRoot(projectRoot string) (string, error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", fmt.Errorf("snapshot: resolve project root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("snapshot: eval symlinks: %w", err)
		}
		real = abs
	}
	return real, nil
}

func sanitizeSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "session"
	}
	replacer := strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", " ", "-")
	return replacer.Replace(id)
}
