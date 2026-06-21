package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/undeadindustries/sagittarius/internal/agents"
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
	workDir  string
}

// RuntimeConfig configures session-scoped MCP/skills/extensions services.
type RuntimeConfig struct {
	Settings      *config.Settings
	WorkDir       string
	ClientName    string
	ClientVersion string
	Trusted       bool
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
	catalog, err := NewCatalog(CatalogConfig{
		Workspace:  ws,
		MCP:        mcp.NewManager(mcp.ManagerConfig{ClientName: cfg.ClientName, ClientVersion: cfg.ClientVersion}),
		Skills:     skillMgr,
		Extensions: extLoader,
		Settings:   cfg.Settings,
		ClientName: cfg.ClientName,
		Version:    cfg.ClientVersion,
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
		workDir:  ws.Root(),
	}
	if _, err := rt.Agents.Reload(ctx, extLoader.ActiveAgents()); err != nil {
		return nil, fmt.Errorf("initial agent reload: %w", err)
	}
	return rt, nil
}

// ReloadTools reloads extensions, MCP, skills, and returns a fresh registry.
func (r *Runtime) ReloadTools(ctx context.Context) (*tools.Registry, error) {
	if r == nil || r.Catalog == nil {
		return nil, fmt.Errorf("runtime catalog unavailable")
	}
	return r.Catalog.Reload(ctx)
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

// Close releases MCP connections.
func (r *Runtime) Close() error {
	if r == nil || r.Catalog == nil {
		return nil
	}
	return r.Catalog.Close()
}
