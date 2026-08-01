package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
)

// Pool manages a collection of LSP clients, keyed by root directory and command.
type Pool struct {
	mu      sync.Mutex
	clients map[string]*poolEntry

	// MaxClients limits how many language servers we keep alive.
	MaxClients int
}

// poolEntry tracks one pooled client's lifecycle. ready is closed once client
// (and possibly err) have been set, so concurrent callers requesting the same
// key can wait for the in-flight Start instead of racing to start their own.
type poolEntry struct {
	client *Client
	err    error
	ready  chan struct{}
	last   time.Time
}

// NewPool creates a new LSP client pool.
func NewPool() *Pool {
	return &Pool{
		clients:    make(map[string]*poolEntry),
		MaxClients: 3, // Reasonable default for a CLI
	}
}

// GetOrCreate returns an existing client or starts a new one. The server
// process is started off the pool mutex so a slow `initialize` handshake for
// one language never blocks lookups/starts for another.
func (p *Pool) GetOrCreate(ctx context.Context, rootDir string, spec *diagnostics.ServerSpec) (*Client, error) {
	if spec == nil || spec.Command == "" {
		return nil, fmt.Errorf("invalid server spec")
	}

	key := fmt.Sprintf("%s:%s", spec.Command, rootDir)

	p.mu.Lock()
	if entry, ok := p.clients[key]; ok {
		entry.last = time.Now()
		p.mu.Unlock()
		<-entry.ready
		return entry.client, entry.err
	}

	if len(p.clients) >= p.MaxClients {
		p.evictOldestReadyLocked()
	}

	entry := &poolEntry{ready: make(chan struct{}), last: time.Now()}
	p.clients[key] = entry
	p.mu.Unlock()

	client, err := Start(ctx, rootDir, spec.Command, spec.Args...)
	entry.client, entry.err = client, err
	close(entry.ready)

	if err != nil {
		p.mu.Lock()
		if p.clients[key] == entry {
			delete(p.clients, key)
		}
		p.mu.Unlock()
	}

	return client, err
}

// evictOldestReadyLocked closes and removes the least-recently-used client
// that has finished starting, to stay under MaxClients. Entries still
// starting are skipped so eviction never races a concurrent Start. p.mu must
// be held by the caller.
func (p *Pool) evictOldestReadyLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, v := range p.clients {
		select {
		case <-v.ready:
		default:
			continue // still starting; never evict
		}
		if first || v.last.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.last
			first = false
		}
	}
	if oldestKey != "" {
		old := p.clients[oldestKey].client
		delete(p.clients, oldestKey)
		if old != nil {
			go func() {
				if err := old.Close(); err != nil {
					slog.Debug("lsp: close evicted client", "error", err)
				}
			}()
		}
	}
}

// Close shuts down all managed clients.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for k, entry := range p.clients {
		entry := entry
		delete(p.clients, k)
		go func() {
			<-entry.ready
			if entry.client != nil {
				if err := entry.client.Close(); err != nil {
					slog.Debug("lsp: close pooled client", "error", err)
				}
			}
		}()
	}
}

// PoolDiagnoser implements diagnostics.Diagnoser by resolving the correct
// server from the pool for each file's language during the collection run. It
// allows the collector to abstract away LSP client lifecycles while still
// having the pool start and query the right servers.
type PoolDiagnoser struct {
	pool *Pool
}

// NewPoolDiagnoser builds a PoolDiagnoser backed by pool. The workspace/module
// root is supplied per call to Diagnostics, not fixed at construction, so one
// PoolDiagnoser can serve a Collect run spanning multiple nested modules.
func NewPoolDiagnoser(pool *Pool) *PoolDiagnoser {
	return &PoolDiagnoser{pool: pool}
}

// Diagnostics implements diagnostics.Diagnoser.
func (p *PoolDiagnoser) Diagnostics(ctx context.Context, root string, absPaths []string) ([]diagnostics.Finding, error) {
	// First group paths by language so we can query the right server.
	type serverGroup struct {
		spec  *diagnostics.ServerSpec
		paths []string
	}

	groups := make(map[string]*serverGroup)

	for _, path := range absPaths {
		lang := diagnostics.FindLanguage(path)
		if lang == nil || lang.Server == nil {
			continue
		}

		key := lang.Server.Command
		if g, ok := groups[key]; ok {
			g.paths = append(g.paths, path)
		} else {
			groups[key] = &serverGroup{
				spec:  lang.Server,
				paths: []string{path},
			}
		}
	}

	var allFindings []diagnostics.Finding
	var firstErr error

	for _, g := range groups {
		client, err := p.pool.GetOrCreate(ctx, root, g.spec)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		findings, err := client.Diagnostics(ctx, g.paths)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		allFindings = append(allFindings, findings...)
	}

	return allFindings, firstErr
}
