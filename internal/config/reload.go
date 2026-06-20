package config

import "sync"

// ReloadNotifier is a stub hook for live settings reload (Phase 09 slash commands).
// Subscribe receives a signal when settings are reloaded from disk.
type ReloadNotifier struct {
	mu   sync.RWMutex
	ch   chan struct{}
	subs []func()
}

// NewReloadNotifier creates a notifier with a buffered update channel.
func NewReloadNotifier() *ReloadNotifier {
	return &ReloadNotifier{ch: make(chan struct{}, 1)}
}

// Subscribe returns a channel that receives reload signals.
func (n *ReloadNotifier) Subscribe() <-chan struct{} {
	if n == nil {
		ch := make(chan struct{})
		return ch
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.ch
}

// OnReload registers a callback invoked on each reload notification.
func (n *ReloadNotifier) OnReload(fn func()) {
	if n == nil || fn == nil {
		return
	}
	n.mu.Lock()
	n.subs = append(n.subs, fn)
	n.mu.Unlock()
}

// Notify signals all subscribers that settings were reloaded.
func (n *ReloadNotifier) Notify() {
	if n == nil {
		return
	}
	n.mu.RLock()
	subs := make([]func(), len(n.subs))
	copy(subs, n.subs)
	n.mu.RUnlock()

	select {
	case n.ch <- struct{}{}:
	default:
	}

	for _, fn := range subs {
		fn()
	}
}
