package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// Manager owns MCP server connections and discovered tools.
type Manager struct {
	mu        sync.Mutex
	connector Connector
	clients   map[string]*Client
	tools     []*DiscoveredTool
	states    []ServerState
}

// ManagerConfig configures MCP discovery.
type ManagerConfig struct {
	ClientName    string
	ClientVersion string
	Connector     Connector
}

// NewManager constructs an MCP manager with the default SDK connector.
func NewManager(cfg ManagerConfig) *Manager {
	connector := cfg.Connector
	if connector == nil {
		connector = &SDKConnector{
			ClientName:    cfg.ClientName,
			ClientVersion: cfg.ClientVersion,
		}
	}
	return &Manager{
		connector: connector,
		clients:   make(map[string]*Client),
	}
}

// Reload disconnects existing servers and reconnects from settings.
func (m *Manager) Reload(ctx context.Context, servers map[string]config.MCPServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.closeLocked()
	if len(servers) == 0 {
		m.tools = nil
		m.states = nil
		return nil
	}

	var discovered []*DiscoveredTool
	var states []ServerState
	for name, raw := range servers {
		cfg := FromSettings(name, raw)
		if cfg.Disabled {
			states = append(states, ServerState{Name: name, Status: ServerDisabled, Config: cfg})
			continue
		}
		client, err := NewClient(ctx, cfg, m.connector)
		state := client.State(0)
		if err != nil {
			slog.Warn("mcp server connect failed", "server", name, "error", err)
			states = append(states, state)
			m.clients[name] = client
			continue
		}
		mcpTools, err := client.ListTools(ctx)
		if err != nil {
			slog.Warn("mcp list tools failed", "server", name, "error", err)
			state.LastError = err.Error()
			state.Status = ServerDisconnected
			states = append(states, state)
			m.clients[name] = client
			continue
		}
		for _, tool := range mcpTools {
			discovered = append(discovered, newDiscoveredTool(client, tool))
		}
		state = client.State(len(mcpTools))
		states = append(states, state)
		m.clients[name] = client
	}
	m.tools = discovered
	m.states = states
	return nil
}

// Tools returns discovered MCP tools.
func (m *Manager) Tools() []*DiscoveredTool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*DiscoveredTool, len(m.tools))
	copy(out, m.tools)
	return out
}

// States returns the last known server states.
func (m *Manager) States() []ServerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerState, len(m.states))
	copy(out, m.states)
	return out
}

// Close disconnects all MCP servers.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closeLocked()
}

func (m *Manager) closeLocked() error {
	var first error
	for name, client := range m.clients {
		if err := client.Close(); err != nil && first == nil {
			first = fmt.Errorf("close mcp server %q: %w", name, err)
		}
	}
	m.clients = make(map[string]*Client)
	m.tools = nil
	m.states = nil
	return first
}
