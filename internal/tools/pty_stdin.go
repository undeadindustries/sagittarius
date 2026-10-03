package tools

import (
	"context"
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
	mu      sync.Mutex
	f       *os.File
	focused bool
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

// SetFocused sets whether the user is actively focused on this PTY card
// in the UI. When focused, auto-backgrounding timers are suspended.
func (s *PTYStdin) SetFocused(focused bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.focused = focused
	s.mu.Unlock()
}

// Focused returns whether the user is actively focused on this PTY card.
func (s *PTYStdin) Focused() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.focused
}

func (s *PTYStdin) attach(f *os.File) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.f = f
	s.mu.Unlock()
}

// AttachForTesting attaches an arbitrary file handle for unit tests.
func (s *PTYStdin) AttachForTesting(f *os.File) {
	s.attach(f)
}

func (s *PTYStdin) detach() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.f = nil
	s.mu.Unlock()
}

// DetachForTesting detaches the file handle for unit tests.
func (s *PTYStdin) DetachForTesting() {
	s.detach()
}

type ptyStdinContextKey struct{}

// WithPTYStdin returns a context holding stdin so ExecuteStream can pass
// it to the shell tool runner.
func WithPTYStdin(ctx context.Context, stdin *PTYStdin) context.Context {
	if stdin == nil || ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, ptyStdinContextKey{}, stdin)
}

// ptyStdinFrom extracts a PTYStdin stored by WithPTYStdin, or nil.
func ptyStdinFrom(ctx context.Context) *PTYStdin {
	if ctx == nil {
		return nil
	}
	stdin, _ := ctx.Value(ptyStdinContextKey{}).(*PTYStdin)
	return stdin
}
