package tools

import (
	"errors"
	"os"
	"sync"
)

// ErrPTYClosed is returned when writing to a PTY that is not attached or
// has already finished.
var ErrPTYClosed = errors.New("pty: not attached")

// PTYStdin is a thread-safe write handle to a live PTY master. The shell
// runner attaches the master after Start and detaches when the process ends.
type PTYStdin struct {
	mu sync.Mutex
	f  *os.File
}

// Write writes p to the attached PTY master.
func (s *PTYStdin) Write(p []byte) (int, error) {
	if s == nil {
		return 0, ErrPTYClosed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return 0, ErrPTYClosed
	}
	return s.f.Write(p)
}

func (s *PTYStdin) attach(f *os.File) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.f = f
	s.mu.Unlock()
}

func (s *PTYStdin) detach() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.f = nil
	s.mu.Unlock()
}
