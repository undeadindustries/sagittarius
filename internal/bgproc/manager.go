package bgproc

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

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

// Manager tracks processes backgrounded during a session.
type Manager struct {
	mu        sync.RWMutex
	processes map[int]*Process
	ordered   []int
}

// NewManager creates a new background process manager.
func NewManager() *Manager {
	return &Manager{
		processes: make(map[int]*Process),
	}
}

// Register adds or updates a process in the registry.
func (m *Manager) Register(pid, pgid int, command, logPath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.processes[pid]; ok {
		p.Status = StatusRunning
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
	
	// Start a reaper to watch for process exit
	go m.reaper(pid)
}

// reaper waits for the process to exit and updates its status.
// We use os.FindProcess and Wait if it's our direct child, but if it's a
// grandchild, Wait might fail. So we poll with kill(pid, 0).
func (m *Manager) reaper(pid int) {
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			// Process is gone
			m.mu.Lock()
			if p, ok := m.processes[pid]; ok {
				p.Status = StatusExited
				p.ExitCode = -1 // We don't have the real exit code for grandchildren
			}
			m.mu.Unlock()
			return
		}
		time.Sleep(1 * time.Second)
	}
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
	m.mu.RLock()
	p, ok := m.processes[pid]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("process %d not found", pid)
	}
	if p.Status != StatusRunning {
		return nil
	}
	if p.PGID > 0 {
		return syscall.Kill(-p.PGID, syscall.SIGKILL)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// Output reads the tail of the log file for the given process.
func (m *Manager) Output(pid int) string {
	m.mu.RLock()
	p, ok := m.processes[pid]
	m.mu.RUnlock()
	if !ok || p.LogPath == "" {
		return ""
	}
	data, err := os.ReadFile(p.LogPath)
	if err != nil {
		return ""
	}
	// Note: We might want to strip ANSI here if needed, but for the viewer,
	// raw strings might be okay, or we can just return it. 
	// The dialog can handle it.
	return strings.TrimSpace(string(data))
}
