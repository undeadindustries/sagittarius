package agent

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
)

// MCPDialogDeps returns the side-effect adapter the /mcp wizard uses. It is
// consumed by the Bubble Tea layer when opening the MCP overlay.
func (a *App) MCPDialogDeps() mcpdialog.Deps {
	return &mcpDialogDeps{baseDialogDeps{app: a}}
}

// mcpDialogDeps implements mcpdialog.Deps over the App's settings, loader,
// runtime catalog, and credential store.
type mcpDialogDeps struct{ baseDialogDeps }

func (d *mcpDialogDeps) settings() *config.Settings { return d.app.deps.Settings }
func (d *mcpDialogDeps) loader() *config.Loader     { return d.app.deps.Loader }
func (d *mcpDialogDeps) docs() *config.Documents    { return d.app.docs }

// serverScope returns the scope that owns the named server: Project if it is
// defined in the project settings file, Global otherwise.
func (d *mcpDialogDeps) serverScope(name string) config.SettingScope {
	docs := d.docs()
	if docs != nil && docs.Project != nil {
		if servers, err := docs.Project.MCPServers(); err == nil {
			if _, ok := servers[name]; ok {
				return config.ScopeProject
			}
		}
	}
	return config.ScopeGlobal
}

func (d *mcpDialogDeps) ListServers() []mcpdialog.ServerEntry {
	// Read merged settings (project overrides global via shallow merge).
	docs := d.docs()
	merged := d.settings()
	if docs != nil {
		merged = docs.Merged()
	}

	settingsServers := map[string]config.MCPServerConfig{}
	if merged != nil {
		if got, err := merged.MCPServers(); err == nil {
			settingsServers = got
		}
	}

	// Build a project-server set so we can badge entries with their scope.
	projectServers := map[string]struct{}{}
	if docs != nil && docs.Project != nil {
		if got, err := docs.Project.MCPServers(); err == nil {
			for name := range got {
				projectServers[name] = struct{}{}
			}
		}
	}

	extServers := map[string]config.MCPServerConfig{}
	statusByName := map[string]mcp.ServerState{}
	if d.app.runtime != nil && d.app.runtime.Catalog != nil {
		extServers = d.app.runtime.Catalog.ExtensionLoader().ActiveMCPServers()
		for _, st := range d.app.runtime.Catalog.MCPManager().States() {
			statusByName[st.Name] = st
		}
	}

	entries := make([]mcpdialog.ServerEntry, 0, len(settingsServers)+len(extServers))
	for name, cfg := range settingsServers {
		scope := config.ScopeGlobal
		if _, inProject := projectServers[name]; inProject {
			scope = config.ScopeProject
		}
		e := mcpServerEntry(name, cfg, statusByName[name], true, "settings")
		e.Scope = scope
		entries = append(entries, e)
	}
	for name, cfg := range extServers {
		if _, ok := settingsServers[name]; ok {
			continue // a settings entry of the same name takes precedence
		}
		entries = append(entries, mcpServerEntry(name, cfg, statusByName[name], false, "extension"))
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

func mcpServerEntry(name string, cfg config.MCPServerConfig, state mcp.ServerState, editable bool, source string) mcpdialog.ServerEntry {
	return mcpdialog.ServerEntry{
		Name:      name,
		Transport: mcpTransport(cfg),
		Detail:    mcpDetail(cfg),
		Status:    string(state.Status),
		ToolCount: state.ToolCount,
		Disabled:  cfg.Disabled != nil && *cfg.Disabled,
		Editable:  editable,
		Source:    source,
	}
}

func (d *mcpDialogDeps) GetServer(name string) (mcpdialog.ServerForm, bool) {
	// Read from merged so project-scoped servers are visible too.
	docs := d.docs()
	s := d.settings()
	if docs != nil {
		s = docs.Merged()
	}
	if s == nil {
		return mcpdialog.ServerForm{}, false
	}
	servers, err := s.MCPServers()
	if err != nil {
		return mcpdialog.ServerForm{}, false
	}
	cfg, ok := servers[name]
	if !ok {
		return mcpdialog.ServerForm{}, false
	}
	return formFromConfig(name, cfg), true
}

func (d *mcpDialogDeps) SaveServer(ctx context.Context, originalName string, form mcpdialog.ServerForm, scope config.SettingScope) error {
	docs := d.docs()
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	name := strings.TrimSpace(form.Name)
	if name == "" {
		return fmt.Errorf("server name is required")
	}
	cfg, err := configFromForm(form)
	if err != nil {
		return err
	}
	renamed := originalName != "" && originalName != name
	target := docs.TargetSettings(scope)
	if renamed {
		_ = target.RemoveMCPServer(originalName)
		// If the original was in a different scope, clean it up there too.
		other := docs.TargetSettings(1 - scope)
		_ = other.RemoveMCPServer(originalName)
	}
	if err := target.SetMCPServer(name, cfg); err != nil {
		return err
	}
	if err := docs.Save(scope); err != nil {
		return err
	}
	if err := d.persistMCPBearer(ctx, originalName, name, form.Bearer, renamed); err != nil {
		return err
	}
	_, err = d.app.deps.Hooks.ReloadMCP(ctx)
	return err
}

// persistMCPBearer stores the server's bearer token and, on rename, keeps the
// secret consistent with the new server name: it migrates the existing token to
// the new key when no new secret was entered, and always clears the stale token
// keyed under the old name so the renamed server stays authenticated and no
// orphaned secret is left behind.
func (d *mcpDialogDeps) persistMCPBearer(ctx context.Context, originalName, name, bearer string, renamed bool) error {
	bearer = strings.TrimSpace(bearer)
	if bearer == "" && renamed {
		if existing, err := credentials.ResolveMCPServerBearer(ctx, originalName); err == nil {
			bearer = strings.TrimSpace(existing)
		}
	}
	if bearer != "" {
		if err := credentials.SetMCPServerBearer(ctx, name, bearer); err != nil {
			return fmt.Errorf("server saved but bearer store failed: %w", err)
		}
	}
	if renamed {
		// Best-effort cleanup; the old key may not have held a token.
		_ = credentials.DeleteMCPServerBearer(ctx, originalName)
	}
	return nil
}

func (d *mcpDialogDeps) RemoveServer(ctx context.Context, name string) error {
	docs := d.docs()
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	scope := d.serverScope(name)
	target := docs.TargetSettings(scope)
	if err := target.RemoveMCPServer(name); err != nil {
		return err
	}
	if err := docs.Save(scope); err != nil {
		return err
	}
	_ = credentials.DeleteMCPServerBearer(ctx, name)
	_, err := d.app.deps.Hooks.ReloadMCP(ctx)
	return err
}

func (d *mcpDialogDeps) SetDisabled(ctx context.Context, name string, disabled bool) error {
	docs := d.docs()
	if docs == nil {
		return fmt.Errorf("settings not loaded")
	}
	scope := d.serverScope(name)
	target := docs.TargetSettings(scope)
	if err := target.SetMCPServerDisabled(name, disabled); err != nil {
		return err
	}
	if err := docs.Save(scope); err != nil {
		return err
	}
	_, err := d.app.deps.Hooks.ReloadMCP(ctx)
	return err
}

func (d *mcpDialogDeps) Reload(ctx context.Context) (string, error) {
	return d.app.deps.Hooks.ReloadMCP(ctx)
}

// mcpTransport classifies a server config into a transport label for display
// and form editing.
func mcpTransport(cfg config.MCPServerConfig) string {
	switch {
	case strings.TrimSpace(cfg.Command) != "":
		return mcpdialog.TransportStdio
	case cfg.Type == "sse" || strings.Contains(cfg.URL, "/sse"):
		return mcpdialog.TransportSSE
	case strings.TrimSpace(cfg.HTTPURL) != "" || strings.TrimSpace(cfg.URL) != "":
		return mcpdialog.TransportHTTP
	default:
		return mcpdialog.TransportStdio
	}
}

func mcpDetail(cfg config.MCPServerConfig) string {
	if strings.TrimSpace(cfg.Command) != "" {
		if len(cfg.Args) > 0 {
			return cfg.Command + " " + strings.Join(cfg.Args, " ")
		}
		return cfg.Command
	}
	if cfg.HTTPURL != "" {
		return cfg.HTTPURL
	}
	return cfg.URL
}

func formFromConfig(name string, cfg config.MCPServerConfig) mcpdialog.ServerForm {
	form := mcpdialog.ServerForm{
		Name:        name,
		Transport:   mcpTransport(cfg),
		Command:     cfg.Command,
		Args:        strings.Join(cfg.Args, " "),
		Env:         joinKV(cfg.Env),
		Headers:     joinKV(cfg.Headers),
		Description: cfg.Description,
		Trust:       cfg.Trust != nil && *cfg.Trust,
		Disabled:    cfg.Disabled != nil && *cfg.Disabled,
	}
	if cfg.HTTPURL != "" {
		form.URL = cfg.HTTPURL
	} else {
		form.URL = cfg.URL
	}
	if cfg.Timeout != nil {
		form.Timeout = strconv.Itoa(*cfg.Timeout)
	}
	return form
}

func configFromForm(form mcpdialog.ServerForm) (config.MCPServerConfig, error) {
	cfg := config.MCPServerConfig{Description: strings.TrimSpace(form.Description)}

	env, err := parseKV(form.Env)
	if err != nil {
		return cfg, fmt.Errorf("env: %w", err)
	}
	cfg.Env = env

	switch form.Transport {
	case mcpdialog.TransportHTTP, mcpdialog.TransportSSE:
		url := strings.TrimSpace(form.URL)
		if url == "" {
			return cfg, fmt.Errorf("URL is required for %s transport", form.Transport)
		}
		headers, err := parseKV(form.Headers)
		if err != nil {
			return cfg, fmt.Errorf("headers: %w", err)
		}
		cfg.Headers = headers
		if form.Transport == mcpdialog.TransportSSE {
			cfg.URL = url
			cfg.Type = "sse"
		} else {
			cfg.HTTPURL = url
			cfg.Type = "http"
		}
	default:
		command := strings.TrimSpace(form.Command)
		if command == "" {
			return cfg, fmt.Errorf("command is required for stdio transport")
		}
		cfg.Command = command
		cfg.Args = splitArgs(form.Args)
	}

	if t := strings.TrimSpace(form.Timeout); t != "" {
		ms, err := strconv.Atoi(t)
		if err != nil || ms < 0 {
			return cfg, fmt.Errorf("timeout must be a non-negative integer (milliseconds)")
		}
		cfg.Timeout = &ms
	}
	if form.Trust {
		trust := true
		cfg.Trust = &trust
	}
	if form.Disabled {
		disabled := true
		cfg.Disabled = &disabled
	}
	return cfg, nil
}

func splitArgs(s string) []string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func joinKV(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

func parseKV(s string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid pair %q (want K=V)", pair)
		}
		out[key] = strings.TrimSpace(value)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
