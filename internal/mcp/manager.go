package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// maxConcurrentMCPConnects bounds the connect/list fan-out during Reload and
// ToolInventory. MCP servers are external processes/HTTP endpoints, so this caps
// resource use without serializing independent round trips.
const maxConcurrentMCPConnects = 8

// Manager owns MCP server connections and discovered tools.
type Manager struct {
	mu        sync.Mutex
	connector Connector
	clients   map[string]*Client
	// allTools is the unfiltered discovery cache (every tool every connected
	// server exposed at the last Reload). tools is the active subset after
	// applying each server's include/exclude filter. Caching the unfiltered set
	// lets ApplyToolFilters re-derive tools on a filter toggle with no network.
	allTools []*DiscoveredTool
	tools    []*DiscoveredTool
	states   []ServerState
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

// serverResult is one server's reload outcome, computed off-lock by connectOne.
type serverResult struct {
	name   string
	client *Client
	state  ServerState
	tools  []*DiscoveredTool
}

// Reload disconnects existing servers and reconnects from settings. Connections
// and tool discovery run concurrently (bounded) off-lock; the manager mutex is
// held only to tear down the old clients and to publish the new maps, so
// Tools/States/tool-execution callers never block for the full reload duration.
func (m *Manager) Reload(ctx context.Context, servers map[string]config.MCPServerConfig) error {
	// 1. Tear down existing connections under the lock, then release it.
	m.mu.Lock()
	_ = m.closeLocked()
	m.mu.Unlock()

	if len(servers) == 0 {
		return nil
	}

	// 2. Connect + list tools concurrently, off-lock. Per-server failures are
	// recorded in ServerState and never abort siblings, so the closures return
	// nil and errgroup is used only for lifecycle and bounding.
	results := make([]serverResult, 0, len(servers))
	var resultsMu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentMCPConnects)
	for name, raw := range servers {
		name, raw := name, raw
		g.Go(func() error {
			res := connectOne(gctx, name, FromSettings(name, raw), m.connector)
			resultsMu.Lock()
			results = append(results, res)
			resultsMu.Unlock()
			return nil
		})
	}
	_ = g.Wait()

	// 3. Re-acquire the lock briefly to publish the new maps in deterministic
	// (name-sorted) order so States()/tool ordering is stable for callers/tests.
	sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients = make(map[string]*Client, len(results))
	var discovered []*DiscoveredTool
	var states []ServerState
	for _, r := range results {
		if r.client != nil {
			m.clients[r.name] = r.client
		}
		states = append(states, r.state)
		discovered = append(discovered, r.tools...)
	}
	m.allTools = discovered
	m.states = states
	// Derive the active (filtered) set + ToolCounts from the unfiltered cache.
	m.applyToolFiltersLocked(servers)
	return nil
}

// connectOne connects to a single server and lists every tool it exposes
// (unfiltered). It is pure and lock-free: the outcome (including any failure) is
// returned in serverResult so the caller can publish results without holding a
// lock during network I/O. Include/exclude filtering is applied later, off this
// path, so a filter toggle never requires a reconnect.
func connectOne(ctx context.Context, name string, cfg ServerConfig, connector Connector) serverResult {
	if cfg.Disabled {
		return serverResult{name: name, state: ServerState{Name: name, Status: ServerDisabled, Config: cfg}}
	}
	client, err := NewClient(ctx, cfg, connector)
	if err != nil {
		slog.Warn("mcp server connect failed", "server", name, "error", err)
		return serverResult{name: name, client: client, state: client.State(0)}
	}
	mcpTools, err := client.ListAllTools(ctx)
	if err != nil {
		slog.Warn("mcp list tools failed", "server", name, "error", err)
		// Tool discovery failed on a live session; close it so the stdio child
		// process / HTTP connection is not leaked until the next reload.
		if closeErr := client.Close(); closeErr != nil {
			slog.Warn("mcp close after list failure", "server", name, "error", closeErr)
		}
		state := client.State(0)
		state.LastError = err.Error()
		state.Status = ServerDisconnected
		return serverResult{name: name, client: client, state: state}
	}
	discovered := make([]*DiscoveredTool, 0, len(mcpTools))
	for _, tool := range mcpTools {
		discovered = append(discovered, newDiscoveredTool(client, tool))
	}
	// ToolCount is set by applyToolFiltersLocked once the filter is known.
	return serverResult{name: name, client: client, state: client.State(0), tools: discovered}
}

// ApplyToolFilters re-derives the active tool set from the unfiltered discovery
// cache using the include/exclude filters in servers, with no network I/O and no
// reconnect. It refreshes each connected server's stored filter config and
// ToolCount. Use for settings-only tool filter toggles, where connections are
// unchanged.
func (m *Manager) ApplyToolFilters(servers map[string]config.MCPServerConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyToolFiltersLocked(servers)
}

// applyToolFiltersLocked recomputes m.tools from m.allTools and refreshes each
// server's filter config + ToolCount. The caller must hold m.mu.
func (m *Manager) applyToolFiltersLocked(servers map[string]config.MCPServerConfig) {
	counts := make(map[string]int)
	filtered := make([]*DiscoveredTool, 0, len(m.allTools))
	for _, dt := range m.allTools {
		cfg := FromSettings(dt.serverName, servers[dt.serverName])
		if toolNameAllowed(dt.toolName, cfg.IncludeTools, cfg.ExcludeTools) {
			filtered = append(filtered, dt)
			counts[dt.serverName]++
		}
	}
	m.tools = filtered
	for i := range m.states {
		st := &m.states[i]
		if raw, ok := servers[st.Name]; ok {
			st.Config = FromSettings(st.Name, raw)
		}
		if st.Status == ServerConnected {
			st.ToolCount = counts[st.Name]
		}
	}
}

// toolNameAllowed reports whether a tool passes a server's include/exclude
// filter: a non-empty include list is an allowlist; exclude always blocks.
func toolNameAllowed(name string, include, exclude []string) bool {
	if len(include) > 0 {
		if _, ok := toSet(include)[name]; !ok {
			return false
		}
	}
	if _, blocked := toSet(exclude)[name]; blocked {
		return false
	}
	return true
}

// Tools returns discovered MCP tools.
func (m *Manager) Tools() []*DiscoveredTool {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*DiscoveredTool, len(m.tools))
	copy(out, m.tools)
	return out
}

// ToolInfo describes one MCP tool for the inventory UI.
type ToolInfo struct {
	Name        string // native tool name reported by the server
	WireName    string // qualified mcp_{server}_{tool} name
	Description string
	Enabled     bool // passes the server's include/exclude filter
}

// ServerToolInventory groups a server's tools with its connection status.
type ServerToolInventory struct {
	Server string
	Status ServerStatus
	Err    string
	Tools  []ToolInfo
}

// ToolInventory returns the full (unfiltered) tool list per server with each
// tool's enabled state computed from include/exclude filters. Connected servers
// are queried on demand; disconnected or disabled servers report their status
// with no tools. The manager lock is released before any network I/O.
func (m *Manager) ToolInventory(ctx context.Context) []ServerToolInventory {
	m.mu.Lock()
	states := make([]ServerState, len(m.states))
	copy(states, m.states)
	clients := make(map[string]*Client, len(m.clients))
	for name, client := range m.clients {
		clients[name] = client
	}
	m.mu.Unlock()

	// Query connected servers concurrently. Writing each result to its own slot
	// preserves the states ordering without a post-sort. States is already
	// name-sorted (it is published sorted by Reload); sort defensively below in
	// case a caller mutates ordering expectations.
	out := make([]ServerToolInventory, len(states))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrentMCPConnects)
	for i, st := range states {
		i, st := i, st
		g.Go(func() error {
			inv := ServerToolInventory{Server: st.Name, Status: st.Status, Err: st.LastError}
			if client := clients[st.Name]; client != nil && st.Status == ServerConnected {
				all, err := client.ListAllTools(gctx)
				if err != nil {
					inv.Err = err.Error()
				} else {
					infos := toolInfos(st.Name, all, st.Config.IncludeTools, st.Config.ExcludeTools)
					sort.Slice(infos, func(a, b int) bool {
						return infos[a].Name < infos[b].Name
					})
					inv.Tools = infos
				}
			}
			out[i] = inv
			return nil
		})
	}
	_ = g.Wait()

	sort.Slice(out, func(i, j int) bool {
		return out[i].Server < out[j].Server
	})
	return out
}

func toolInfos(server string, tools []*sdkmcp.Tool, include, exclude []string) []ToolInfo {
	includeSet := toSet(include)
	excludeSet := toSet(exclude)
	out := make([]ToolInfo, 0, len(tools))
	for _, tool := range tools {
		enabled := true
		if len(includeSet) > 0 {
			_, enabled = includeSet[tool.Name]
		}
		if _, blocked := excludeSet[tool.Name]; blocked {
			enabled = false
		}
		out = append(out, ToolInfo{
			Name:        tool.Name,
			WireName:    FormatToolName(server, tool.Name),
			Description: tool.Description,
			Enabled:     enabled,
		})
	}
	return out
}

// States returns the last known server states, sorted by server name.
func (m *Manager) States() []ServerState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ServerState, len(m.states))
	copy(out, m.states)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
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
	m.allTools = nil
	m.tools = nil
	m.states = nil
	return first
}
