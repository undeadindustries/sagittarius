package tools

import (
	"sort"
	"sync"
	"time"
)

// FileStateRegistry coordinates file access between concurrently running
// agents that share one working tree.
//
// A write lease prevents two subagents from being handed the same paths, but
// it cannot see two other cases: a child writing a file the parent already
// read, and a child writing a file a sibling read before the lease was
// granted. This registry catches both at write time.
//
// Every method is safe on a nil receiver so callers need no branch, and safe
// for concurrent use.
type FileStateRegistry struct {
	metaMu sync.Mutex
	locks  map[string]*sync.Mutex

	stateMu    sync.Mutex
	reads      map[string]map[string]time.Time
	writes     map[string]map[string]time.Time
	lastWriter map[string]writeStamp
}

type writeStamp struct {
	agentID string
	at      time.Time
}

// NewFileStateRegistry returns an empty registry.
func NewFileStateRegistry() *FileStateRegistry {
	return &FileStateRegistry{
		locks:      make(map[string]*sync.Mutex),
		reads:      make(map[string]map[string]time.Time),
		writes:     make(map[string]map[string]time.Time),
		lastWriter: make(map[string]writeStamp),
	}
}

// RecordRead notes that agentID observed absPath at this moment.
func (r *FileStateRegistry) RecordRead(agentID, absPath string) {
	if r == nil || agentID == "" || absPath == "" {
		return
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	stampLocked(r.reads, agentID, absPath, time.Now())
}

// RecordWrite notes that agentID wrote absPath at this moment. The writer's
// own read stamp is refreshed so its next write does not report itself stale.
func (r *FileStateRegistry) RecordWrite(agentID, absPath string) {
	if r == nil || agentID == "" || absPath == "" {
		return
	}
	now := time.Now()
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	r.lastWriter[absPath] = writeStamp{agentID: agentID, at: now}
	stampLocked(r.writes, agentID, absPath, now)
	stampLocked(r.reads, agentID, absPath, now)
}

// WrittenBy returns the paths agentID wrote, sorted. Unlike the last-writer
// record this is per agent, so a later write by someone else does not erase the
// fact that this agent touched the file.
func (r *FileStateRegistry) WrittenBy(agentID string) []string {
	if r == nil || agentID == "" {
		return nil
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	out := make([]string, 0, len(r.writes[agentID]))
	for p := range r.writes[agentID] {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func stampLocked(m map[string]map[string]time.Time, agentID, absPath string, at time.Time) {
	byPath := m[agentID]
	if byPath == nil {
		byPath = make(map[string]time.Time)
		m[agentID] = byPath
	}
	byPath[absPath] = at
}

// CheckStale reports whether another agent wrote absPath after agentID last
// read it, naming that agent. A path this agent never read is not stale: it
// has no prior view to invalidate.
func (r *FileStateRegistry) CheckStale(agentID, absPath string) (string, bool) {
	if r == nil || agentID == "" || absPath == "" {
		return "", false
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	readAt, ok := r.reads[agentID][absPath]
	if !ok {
		return "", false
	}
	last, ok := r.lastWriter[absPath]
	if !ok || last.agentID == agentID {
		return "", false
	}
	if last.at.After(readAt) {
		return last.agentID, true
	}
	return "", false
}

// WritesSince returns the paths agentID had read that a different agent wrote
// after the given time, sorted for stable output. It backs the notice a parent
// receives when a child modified a file the parent was working from.
func (r *FileStateRegistry) WritesSince(agentID string, since time.Time) []string {
	if r == nil || agentID == "" {
		return nil
	}
	r.stateMu.Lock()
	defer r.stateMu.Unlock()
	var out []string
	for p := range r.reads[agentID] {
		last, ok := r.lastWriter[p]
		if !ok || last.agentID == agentID {
			continue
		}
		if last.at.After(since) {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// LockPath serializes the read-modify-write region for one path and returns
// the release function. Different paths never contend.
func (r *FileStateRegistry) LockPath(absPath string) func() {
	if r == nil || absPath == "" {
		return func() {}
	}
	return r.LockPaths([]string{absPath})
}

// LockPaths locks several paths at once. Paths are acquired in sorted order so
// two callers holding overlapping sets can never deadlock, and released in
// reverse.
func (r *FileStateRegistry) LockPaths(absPaths []string) func() {
	if r == nil || len(absPaths) == 0 {
		return func() {}
	}
	ordered := dedupeSorted(absPaths)
	held := make([]*sync.Mutex, 0, len(ordered))
	for _, p := range ordered {
		mu := r.lockFor(p)
		mu.Lock()
		held = append(held, mu)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
}

func (r *FileStateRegistry) lockFor(absPath string) *sync.Mutex {
	r.metaMu.Lock()
	defer r.metaMu.Unlock()
	mu, ok := r.locks[absPath]
	if !ok {
		mu = &sync.Mutex{}
		r.locks[absPath] = mu
	}
	return mu
}

func dedupeSorted(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
