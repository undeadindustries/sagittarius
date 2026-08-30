package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/extensions"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Catalog assembles built-in, MCP, and skill tools into one registry.
type Catalog struct {
	ws                 *tools.Workspace
	mcp                *mcp.Manager
	skills             *skills.Manager
	extensions         *extensions.Loader
	settings           *config.Settings
	bgMgr              *bgproc.Manager
	allowFix           bool
	researchSubagents  bool
	codingSubagents    bool
	editEnabled        bool
	symbolsEnabled     bool
	symbolsPreferGopls bool
	webSearchEnabled   bool
	webFetchEnabled    bool
	webDirectFetch     bool
	webMaxFetchBytes   int
	webUtilityClient   *provider.GeminiUtilityClient
	spillDir           string
	scriptToolEnabled  bool
}

// CatalogConfig configures tool catalog assembly.
type CatalogConfig struct {
	Workspace  *tools.Workspace
	MCP        *mcp.Manager
	Skills     *skills.Manager
	Extensions *extensions.Loader
	Settings   *config.Settings
	BgMgr      *bgproc.Manager
	ClientName string
	Version    string
	// AllowFix permits run_project_checks to run mutating formatters (fix=true).
	AllowFix         bool
	EditEnabled      bool
	PruneToolSchemas bool
	// SymbolsEnabled toggles registration of the find_symbol tool (default true).
	SymbolsEnabled bool
	// SymbolsPreferGopls tweaks find_symbol's description on Go modules.
	SymbolsPreferGopls bool
	// WebSearchEnabled toggles the google_web_search tool.
	WebSearchEnabled bool
	// WebFetchEnabled toggles the web_fetch tool.
	WebFetchEnabled bool
	// SpillDir is where truncated tool outputs are written for later read_file.
	SpillDir string
	// ScriptToolEnabled toggles registration of run_script (default false).
	ScriptToolEnabled bool
	// ResearchSubagentsEnabled and CodingSubagentsEnabled are change-detection
	// state only: the task and code_task tools are registered by the runner
	// (NewRunner and App.SetRegistry), because they need a *Runner. Tracking
	// them here is what makes a live /settings toggle rebuild the registry.
	ResearchSubagentsEnabled bool
	CodingSubagentsEnabled   bool
}

// ResearchSubagentsEnabled reports the catalog's current research-subagent
// toggle, refreshed from settings by RefreshBuiltinToggles.
func (c *Catalog) ResearchSubagentsEnabled() bool { return c != nil && c.researchSubagents }

// CodingSubagentsEnabled reports the catalog's current coding-subagent toggle.
func (c *Catalog) CodingSubagentsEnabled() bool { return c != nil && c.codingSubagents }

// NewCatalog constructs a tool catalog.
func NewCatalog(cfg CatalogConfig) (*Catalog, error) {
	if cfg.Workspace == nil {
		return nil, fmt.Errorf("tool catalog: workspace is required")
	}
	if cfg.MCP == nil {
		cfg.MCP = mcp.NewManager(mcp.ManagerConfig{
			ClientName:       cfg.ClientName,
			ClientVersion:    cfg.Version,
			PruneToolSchemas: cfg.PruneToolSchemas,
		})
	}
	if cfg.Skills == nil {
		cfg.Skills = skills.NewManager(cfg.Workspace.Root(), true)
	}
	if cfg.Extensions == nil {
		cfg.Extensions = extensions.NewLoader()
	}
	var webUtilityClient *provider.GeminiUtilityClient
	if cfg.WebSearchEnabled || cfg.WebFetchEnabled {
		// A missing Gemini key is an expected configuration, not a failure: the
		// registry skips google_web_search when the client is nil and web_fetch
		// falls back to its key-free Go HTTP path. Log at debug so the reason is
		// still recoverable when a user asks why search is absent.
		client, err := provider.NewGeminiUtilityClient(context.Background(),
			config.WebUtilityModel(cfg.Settings, nil))
		if err != nil {
			slog.Debug("web tools: gemini utility client unavailable", "error", err)
		} else {
			webUtilityClient = client
		}
	}
	directFetch := config.WebDirectFetch(cfg.Settings, nil)

	return &Catalog{
		ws:                 cfg.Workspace,
		mcp:                cfg.MCP,
		skills:             cfg.Skills,
		extensions:         cfg.Extensions,
		settings:           cfg.Settings,
		bgMgr:              cfg.BgMgr,
		allowFix:           cfg.AllowFix,
		researchSubagents:  cfg.ResearchSubagentsEnabled,
		codingSubagents:    cfg.CodingSubagentsEnabled,
		editEnabled:        cfg.EditEnabled,
		symbolsEnabled:     cfg.SymbolsEnabled,
		symbolsPreferGopls: cfg.SymbolsPreferGopls,
		webSearchEnabled:   cfg.WebSearchEnabled,
		webFetchEnabled:    cfg.WebFetchEnabled,
		webDirectFetch:     directFetch,
		webMaxFetchBytes:   config.WebMaxFetchBytes(cfg.Settings, nil, directFetch),
		webUtilityClient:   webUtilityClient,
		spillDir:           cfg.SpillDir,
		scriptToolEnabled:  cfg.ScriptToolEnabled,
	}, nil
}

// RefreshBuiltinToggles re-resolves the built-in tool toggles from the given
// settings document and reports whether any of them changed. Callers rebuild the
// registry only on a true result, so a mode switch or model pick stays cheap
// while a /settings toggle still takes effect without a restart.
//
// CatalogConfig's flags seed the initial state; from the first refresh on, the
// settings document is authoritative. main.go derives those flags from the same
// resolvers, so the two agree at startup.
func (c *Catalog) RefreshBuiltinToggles(s *config.Settings) bool {
	if c == nil {
		return false
	}
	next := c.resolveToggles(s)
	changed := next != c.toggles()
	c.applyToggles(next)
	return changed
}

// builtinToggles is the full set of settings-driven built-in tool configuration.
// It is deliberately comparable so change detection is one comparison and adding
// a field cannot silently escape it.
type builtinToggles struct {
	allowFix           bool
	researchSubagents  bool
	codingSubagents    bool
	editEnabled        bool
	symbolsEnabled     bool
	symbolsPreferGopls bool
	webSearchEnabled   bool
	webFetchEnabled    bool
	webDirectFetch     bool
	webMaxFetchBytes   int
	scriptToolEnabled  bool
}

func (c *Catalog) resolveToggles(s *config.Settings) builtinToggles {
	directFetch := config.WebDirectFetch(s, nil)
	return builtinToggles{
		allowFix:           config.VerifyAllowFix(s, nil),
		researchSubagents:  config.ResearchSubagentsEnabled(s, nil),
		codingSubagents:    config.CodingSubagentsEnabled(s, nil),
		editEnabled:        config.EditEnabled(s, nil),
		symbolsEnabled:     config.SymbolsEnabled(s, nil),
		symbolsPreferGopls: config.SymbolsPreferGopls(s, nil),
		webSearchEnabled:   config.WebSearchEnabled(s, nil),
		webFetchEnabled:    config.WebFetchEnabled(s, nil),
		webDirectFetch:     directFetch,
		webMaxFetchBytes:   config.WebMaxFetchBytes(s, nil, directFetch),
		scriptToolEnabled:  config.ScriptToolEnabled(s, nil),
	}
}

func (c *Catalog) toggles() builtinToggles {
	return builtinToggles{
		allowFix:           c.allowFix,
		researchSubagents:  c.researchSubagents,
		codingSubagents:    c.codingSubagents,
		editEnabled:        c.editEnabled,
		symbolsEnabled:     c.symbolsEnabled,
		symbolsPreferGopls: c.symbolsPreferGopls,
		webSearchEnabled:   c.webSearchEnabled,
		webFetchEnabled:    c.webFetchEnabled,
		webDirectFetch:     c.webDirectFetch,
		webMaxFetchBytes:   c.webMaxFetchBytes,
		scriptToolEnabled:  c.scriptToolEnabled,
	}
}

func (c *Catalog) applyToggles(t builtinToggles) {
	c.allowFix = t.allowFix
	c.researchSubagents = t.researchSubagents
	c.codingSubagents = t.codingSubagents
	c.editEnabled = t.editEnabled
	c.symbolsEnabled = t.symbolsEnabled
	c.symbolsPreferGopls = t.symbolsPreferGopls
	c.webSearchEnabled = t.webSearchEnabled
	c.webFetchEnabled = t.webFetchEnabled
	c.webDirectFetch = t.webDirectFetch
	c.webMaxFetchBytes = t.webMaxFetchBytes
	c.scriptToolEnabled = t.scriptToolEnabled
}

// BuildRegistry assembles the current registry without reconnecting MCP servers.
// Built-in tool toggles come from the fields RefreshBuiltinToggles maintains, so
// a /settings change takes effect on the next rebuild without a restart.
func (c *Catalog) BuildRegistry() *tools.Registry {
	reg := tools.NewBuiltinRegistry(c.ws,
		tools.WithAllowFix(c.allowFix),
		tools.WithBackgroundManager(c.bgMgr),
		tools.WithEdit(c.editEnabled),
		tools.WithSymbols(c.symbolsEnabled, c.symbolsPreferGopls),
		tools.WithWebTools(
			c.webSearchEnabled,
			c.webFetchEnabled,
			c.webUtilityClient,
			c.webDirectFetch,
			c.webMaxFetchBytes,
		),
		tools.WithSpillDir(c.spillDir),
		tools.WithScriptTool(c.scriptToolEnabled),
	)
	reg.Register(tools.NewActivateSkillTool(c.skills))
	for _, tool := range c.mcp.Tools() {
		reg.Register(wrapMCPTool(tool))
	}
	return reg
}

// RebuildRegistryWithFilters re-applies each MCP server's include/exclude tool
// filter from the current settings to the already-discovered tool cache (no
// reconnect, no network) and returns a fresh registry. Use for tool-filter
// toggles, where only policy changed and the live connections are unchanged.
func (c *Catalog) RebuildRegistryWithFilters() (*tools.Registry, error) {
	servers, err := c.mergeMCPServers()
	if err != nil {
		return nil, err
	}
	c.mcp.ApplyToolFilters(servers)
	return c.BuildRegistry(), nil
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
	if err := c.mcp.Reload(ctx, servers, config.PruneToolSchemasEnabled(c.settings, nil)); err != nil {
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

// SetSettings updates the settings pointer used by Reload and mergeMCPServers.
// Call before ReloadTools when the active settings document has changed (e.g.
// after a scoped save that updates the merged view).
func (c *Catalog) SetSettings(s *config.Settings) {
	if c != nil {
		c.settings = s
	}
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
