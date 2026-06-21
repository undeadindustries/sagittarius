package agent

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/extensions"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Catalog assembles built-in, MCP, and skill tools into one registry.
type Catalog struct {
	ws         *tools.Workspace
	mcp        *mcp.Manager
	skills     *skills.Manager
	extensions *extensions.Loader
	settings   *config.Settings
}

// CatalogConfig configures tool catalog assembly.
type CatalogConfig struct {
	Workspace  *tools.Workspace
	MCP        *mcp.Manager
	Skills     *skills.Manager
	Extensions *extensions.Loader
	Settings   *config.Settings
	ClientName string
	Version    string
}

// NewCatalog constructs a tool catalog.
func NewCatalog(cfg CatalogConfig) (*Catalog, error) {
	if cfg.Workspace == nil {
		return nil, fmt.Errorf("tool catalog: workspace is required")
	}
	if cfg.MCP == nil {
		cfg.MCP = mcp.NewManager(mcp.ManagerConfig{
			ClientName:    cfg.ClientName,
			ClientVersion: cfg.Version,
		})
	}
	if cfg.Skills == nil {
		cfg.Skills = skills.NewManager(cfg.Workspace.Root(), true)
	}
	if cfg.Extensions == nil {
		cfg.Extensions = extensions.NewLoader()
	}
	return &Catalog{
		ws:         cfg.Workspace,
		mcp:        cfg.MCP,
		skills:     cfg.Skills,
		extensions: cfg.Extensions,
		settings:   cfg.Settings,
	}, nil
}

// BuildRegistry assembles the current registry without reconnecting MCP servers.
func (c *Catalog) BuildRegistry() *tools.Registry {
	reg := tools.NewBuiltinRegistry(c.ws)
	reg.Register(tools.NewActivateSkillTool(c.skills))
	for _, tool := range c.mcp.Tools() {
		reg.Register(wrapMCPTool(tool))
	}
	return reg
}

// Reload refreshes extensions, MCP servers, skills, and returns an assembled registry.
func (c *Catalog) Reload(ctx context.Context) (*tools.Registry, error) {
	if err := c.extensions.Reload(c.settings); err != nil {
		return nil, fmt.Errorf("reload extensions: %w", err)
	}
	servers, err := c.mergeMCPServers()
	if err != nil {
		return nil, err
	}
	if err := c.mcp.Reload(ctx, servers); err != nil {
		return nil, fmt.Errorf("reload mcp: %w", err)
	}
	if err := c.skills.Discover(ctx, c.extensions.ActiveSkills()); err != nil {
		return nil, fmt.Errorf("reload skills: %w", err)
	}
	return c.BuildRegistry(), nil
}

func (c *Catalog) mergeMCPServers() (map[string]config.MCPServerConfig, error) {
	servers := make(map[string]config.MCPServerConfig)
	if c.settings != nil {
		fromSettings, err := c.settings.MCPServers()
		if err != nil {
			return nil, err
		}
		for name, cfg := range fromSettings {
			servers[name] = cfg
		}
	}
	for name, cfg := range c.extensions.ActiveMCPServers() {
		servers[name] = cfg
	}
	return servers, nil
}

// MCPManager exposes the underlying MCP manager for slash status output.
func (c *Catalog) MCPManager() *mcp.Manager { return c.mcp }

// SkillManager exposes the skill manager for slash commands.
func (c *Catalog) SkillManager() *skills.Manager { return c.skills }

// ExtensionLoader exposes the extension loader.
func (c *Catalog) ExtensionLoader() *extensions.Loader { return c.extensions }

// Close releases MCP connections.
func (c *Catalog) Close() error {
	return c.mcp.Close()
}

type mcpToolAdapter struct{ inner *mcp.DiscoveredTool }

func wrapMCPTool(tool *mcp.DiscoveredTool) tools.Tool {
	return &mcpToolAdapter{inner: tool}
}

func (a *mcpToolAdapter) Name() string { return a.inner.Name() }

func (a *mcpToolAdapter) RequiresConfirmation() bool { return a.inner.RequiresConfirmation() }

func (a *mcpToolAdapter) Declaration() provider.ToolDeclaration { return a.inner.Declaration() }

func (a *mcpToolAdapter) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return a.inner.Execute(ctx, args)
}

var _ tools.Tool = (*mcpToolAdapter)(nil)
