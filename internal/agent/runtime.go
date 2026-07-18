package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/extensions"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Runtime wires MCP, skills, extensions, and the tool catalog for one session.
type Runtime struct {
	Catalog  *Catalog
	Agents   *agents.Registry
	Settings *config.Settings
	BgMgr    *bgproc.Manager
	workDir  string
}

// RuntimeConfig configures session-scoped MCP/skills/extensions services.
type RuntimeConfig struct {
	Settings      *config.Settings
	WorkDir       string
	ClientName    string
	ClientVersion string
	Trusted       bool
	// AllowFix permits run_project_checks to run mutating formatters (fix=true).
	AllowFix bool
	// SymbolsEnabled toggles registration of the find_symbol tool (default true).
	SymbolsEnabled bool
	// SymbolsPreferGopls tweaks find_symbol's description on Go modules.
	SymbolsPreferGopls bool
}

// NewRuntime constructs and performs an initial tool catalog reload.
func NewRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	workDir := cfg.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("runtime workdir: %w", err)
		}
	}
	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		return nil, fmt.Errorf("runtime workspace: %w", err)
	}
	extLoader := extensions.NewLoader()
	skillMgr := skills.NewManager(ws.Root(), cfg.Trusted)
	bgMgr := bgproc.NewManager()
	catalog, err := NewCatalog(CatalogConfig{
		Workspace:          ws,
		MCP:                mcp.NewManager(mcp.ManagerConfig{ClientName: cfg.ClientName, ClientVersion: cfg.ClientVersion}),
		Skills:             skillMgr,
		Extensions:         extLoader,
		Settings:           cfg.Settings,
		BgMgr:              bgMgr,
		ClientName:         cfg.ClientName,
		Version:            cfg.ClientVersion,
		AllowFix:           cfg.AllowFix,
		SymbolsEnabled:     cfg.SymbolsEnabled,
		SymbolsPreferGopls: cfg.SymbolsPreferGopls,
	})
	if err != nil {
		return nil, err
	}
	if _, err := catalog.Reload(ctx); err != nil {
		return nil, fmt.Errorf("initial tool reload: %w", err)
	}
	rt := &Runtime{
		Catalog:  catalog,
		Agents:   agents.NewRegistry(ws.Root(), cfg.Trusted),
		Settings: cfg.Settings,
		BgMgr:    bgMgr,
		workDir:  ws.Root(),
	}
	if _, err := rt.Agents.Reload(ctx, extLoader.ActiveAgents()); err != nil {
		return nil, fmt.Errorf("initial agent reload: %w", err)
	}
	return rt, nil
}

// Registry assembles the current tool registry from the already-loaded catalog
// without reconnecting MCP servers. Use after NewRuntime, which performs the
// initial discovery, to avoid a redundant reconnect.
func (r *Runtime) Registry() *tools.Registry {
	if r == nil || r.Catalog == nil {
		return nil
	}
	return r.Catalog.BuildRegistry()
}

// SetSettings updates the settings pointer used by ReloadTools and ReloadSkills.
// Call this whenever the merged settings document changes so that MCP server
// discovery and extension loading see the current configuration.
func (r *Runtime) SetSettings(s *config.Settings) {
	if r == nil {
		return
	}
	r.Settings = s
	r.Catalog.SetSettings(s)
}

// ReloadTools reloads extensions, MCP, skills, and returns a fresh registry.
func (r *Runtime) ReloadTools(ctx context.Context) (*tools.Registry, error) {
	if r == nil || r.Catalog == nil {
		return nil, fmt.Errorf("runtime catalog unavailable")
	}
	return r.Catalog.Reload(ctx)
}

// RebuildToolRegistry re-applies MCP tool include/exclude filters from the
// current settings to the cached tool set and returns a fresh registry without
// reconnecting any MCP server. Use for tool-filter toggles.
func (r *Runtime) RebuildToolRegistry() (*tools.Registry, error) {
	if r == nil || r.Catalog == nil {
		return nil, fmt.Errorf("runtime catalog unavailable")
	}
	return r.Catalog.RebuildRegistryWithFilters()
}

// ReloadSkills rediscovers skills and rebuilds activate_skill declarations.
func (r *Runtime) ReloadSkills(ctx context.Context) (*tools.Registry, error) {
	if r == nil || r.Catalog == nil {
		return nil, fmt.Errorf("runtime catalog unavailable")
	}
	if err := r.Catalog.ExtensionLoader().Reload(r.Settings); err != nil {
		return nil, err
	}
	if err := r.Catalog.SkillManager().Discover(ctx, r.Catalog.ExtensionLoader().ActiveSkills()); err != nil {
		return nil, err
	}
	return r.Catalog.BuildRegistry(), nil
}

// ReloadAgents rediscovers agent definitions.
func (r *Runtime) ReloadAgents(ctx context.Context) (agents.ReloadSummary, error) {
	if r == nil || r.Agents == nil {
		return agents.ReloadSummary{}, fmt.Errorf("runtime agents unavailable")
	}
	if err := r.Catalog.ExtensionLoader().Reload(r.Settings); err != nil {
		return agents.ReloadSummary{}, err
	}
	return r.Agents.Reload(ctx, r.Catalog.ExtensionLoader().ActiveAgents())
}

// Close releases MCP connections and stops the background-process reaper.
func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	if r.BgMgr != nil {
		_ = r.BgMgr.Close()
	}
	if r.Catalog == nil {
		return nil
	}
	return r.Catalog.Close()
}
