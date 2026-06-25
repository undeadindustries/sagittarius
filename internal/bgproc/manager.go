package bgproc

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// tailBytes bounds how much of a process log the viewer loads — only the last
// chunk is interesting and reading a multi-MiB log inline would stall the UI.
const tailBytes = 64 << 10

// ProcessStatus represents the state of a background process.
type ProcessStatus string

const (
	StatusRunning ProcessStatus = "running"
	StatusExited  ProcessStatus = "exited"
)

// Process represents a tracked background process.
type Process struct {
	PID       int
	PGID      int
	Command   string
	StartedAt time.Time
	Status    ProcessStatus
	ExitCode  int
	LogPath   string
}

// reaperInterval is how often the single reaper polls tracked PIDs for exit.
const reaperInterval = time.Second

// Manager tracks processes backgrounded during a session.
type Manager struct {
	mu        sync.RWMutex
	processes map[int]*Process
	ordered   []int

	// One reaper goroutine watches every tracked PID, started lazily on the
	// first Register and cancelled by Close — replacing the previous
	// goroutine-per-PID pollers that never shut down on session teardown.
	ctx  context.Context
	stop context.CancelFunc
	once sync.Once
}

// NewManager creates a new background process manager.
func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		processes: make(map[int]*Process),
		ctx:       ctx,
		stop:      cancel,
	}
}

// Register adds or updates a process in the registry.
func (m *Manager) Register(pid, pgid int, command, logPath string) {
	m.mu.Lock()
	if p, ok := m.processes[pid]; ok {
		p.Status = StatusRunning
		m.mu.Unlock()
		return
	}
	p := &Process{
		PID:       pid,
		PGID:      pgid,
		Command:   command,
		StartedAt: time.Now(),
		Status:    StatusRunning,
		LogPath:   logPath,
	}
	m.processes[pid] = p
	m.ordered = append(m.ordered, pid)
	m.mu.Unlock()

	m.ensureReaper()
}

// ensureReaper starts the single reaper goroutine exactly once.
func (m *Manager) ensureReaper() {
	m.once.Do(func() { go m.reapLoop() })
}

// reapLoop polls all tracked PIDs until the manager is closed. A single
// goroutine watches every process (we poll with kill(pid, 0) because a
// backgrounded grandchild is not our direct child and cannot be Wait()ed).
func (m *Manager) reapLoop() {
	t := time.NewTicker(reaperInterval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.reapOnce()
		}
	}
}

// reapOnce marks any running process whose PID has disappeared as exited.
func (m *Manager) reapOnce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, pid := range m.ordered {
		p, ok := m.processes[pid]
		if !ok || p.Status != StatusRunning {
			continue
		}
		if syscall.Kill(pid, 0) != nil {
			p.markExited()
		}
	}
}

// markExited records a vanished process. The real exit code is unavailable for
// grandchildren, so -1 is used (unchanged from the previous per-PID reaper).
func (p *Process) markExited() {
	p.Status = StatusExited
	p.ExitCode = -1
}

// Close stops the reaper goroutine. Safe to call multiple times; tracked
// process records remain readable after close.
func (m *Manager) Close() error {
	if m.stop != nil {
		m.stop()
	}
	return nil
}

// List returns a snapshot of all tracked processes.
func (m *Manager) List() []Process {
	m.mu.RLock()
	defer m.mu.RUnlock()
	res := make([]Process, 0, len(m.ordered))
	for _, pid := range m.ordered {
		if p, ok := m.processes[pid]; ok {
			res = append(res, *p)
		}
	}
	return res
}

// Get returns details for a specific process.
func (m *Manager) Get(pid int) (Process, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.processes[pid]
	if !ok {
		return Process{}, false
	}
	return *p, true
}

// Kill attempts to terminate a tracked process.
func (m *Manager) Kill(pid int) error {
	// Copy the fields under the lock: the reaper mutates Status concurrently, so
	// reading p.* after releasing the lock would race.
	m.mu.RLock()
	p, ok := m.processes[pid]
	var status ProcessStatus
	var pgid int
	if ok {
		status = p.Status
		pgid = p.PGID
	}
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("process %d not found", pid)
	}
	if status != StatusRunning {
		return nil
	}
	if pgid > 0 {
		return syscall.Kill(-pgid, syscall.SIGKILL)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// Output reads the tail (last tailBytes) of the log file for the given process.
// LogPath is copied under the lock and the file I/O runs off-lock (AD-060), so
// the reaper never blocks on disk. Callers should invoke this from a tea.Cmd,
// not inline on the Bubble Tea Update goroutine.
func (m *Manager) Output(pid int) string {
	m.mu.RLock()
	p, ok := m.processes[pid]
	var logPath string
	if ok {
		logPath = p.LogPath
	}
	m.mu.RUnlock()
	if !ok || logPath == "" {
		return ""
	}
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	if fi, statErr := f.Stat(); statErr == nil && fi.Size() > tailBytes {
		if _, seekErr := f.Seek(-tailBytes, io.SeekEnd); seekErr != nil {
			return ""
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
