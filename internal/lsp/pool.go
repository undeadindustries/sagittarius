package lsp

import (
	"context"
	"fmt"
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

type poolEntry struct {
	client *Client
	last   time.Time
}

// NewPool creates a new LSP client pool.
func NewPool() *Pool {
	return &Pool{
		clients:    make(map[string]*poolEntry),
		MaxClients: 3, // Reasonable default for a CLI
	}
}

// GetOrCreate returns an existing client or starts a new one.
func (p *Pool) GetOrCreate(ctx context.Context, rootDir string, spec *diagnostics.ServerSpec) (*Client, error) {
	if spec == nil || spec.Command == "" {
		return nil, fmt.Errorf("invalid server spec")
	}

	key := fmt.Sprintf("%s:%s", spec.Command, rootDir)

	p.mu.Lock()
	defer p.mu.Unlock()

	if entry, ok := p.clients[key]; ok {
		entry.last = time.Now()
		return entry.client, nil
	}

	// Evict oldest if we hit the cap
	if len(p.clients) >= p.MaxClients {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, v := range p.clients {
			if first || v.last.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.last
				first = false
			}
		}
		if oldestKey != "" {
			old := p.clients[oldestKey].client
			delete(p.clients, oldestKey)
			go old.Close()
		}
	}

	client, err := Start(ctx, rootDir, spec.Command, spec.Args...)
	if err != nil {
		return nil, err
	}

	p.clients[key] = &poolEntry{
		client: client,
		last:   time.Now(),
	}

	return client, nil
}

// Close shuts down all managed clients.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for k, entry := range p.clients {
		go entry.client.Close()
		delete(p.clients, k)
	}
}

// Diagnostics implements the diagnostics.Diagnoser interface by routing the
// request to the appropriate LSP server in the pool.
// PoolDiagnoser implements diagnostics.Diagnoser by resolving the correct server
// from the pool for each file type during the collection run. It allows the
// collector to abstract away LSP client lifecycles while still having the pool
// start and query the right servers.
type PoolDiagnoser struct {
	pool *Pool
	root string
}

func NewPoolDiagnoser(pool *Pool, root string) *PoolDiagnoser {
	return &PoolDiagnoser{pool: pool, root: root}
}

func (p *PoolDiagnoser) Diagnostics(ctx context.Context, absPaths []string) ([]diagnostics.Finding, error) {
	// First group paths by language so we can query the right server
	
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
				spec: lang.Server,
				paths: []string{path},
			}
		}
	}
	
	var allFindings []diagnostics.Finding
	var firstErr error
	
	for _, g := range groups {
		client, err := p.pool.GetOrCreate(ctx, p.root, g.spec)
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