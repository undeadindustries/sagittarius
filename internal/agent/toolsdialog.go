package agent

import (
	"context"
	"fmt"
	"sort"

	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui/toolsdialog"
)

// ToolsDialogDeps returns the side-effect adapter the /tools inventory uses.
func (a *App) ToolsDialogDeps() toolsdialog.Deps {
	return &toolsDialogDeps{app: a}
}

// toolsDialogDeps implements toolsdialog.Deps over the App's runtime catalog and
// settings.
type toolsDialogDeps struct{ app *App }

func (d *toolsDialogDeps) BuiltinTools() []toolsdialog.BuiltinTool {
	if d.app.runtime == nil || d.app.runtime.Catalog == nil {
		return nil
	}
	reg := d.app.runtime.Catalog.BuildRegistry()
	entries := reg.ListEntries()
	out := make([]toolsdialog.BuiltinTool, 0, len(entries))
	for _, e := range entries {
		if e.Source == tools.SourceMCP {
			continue
		}
		out = append(out, toolsdialog.BuiltinTool{
			Name:        e.Name,
			Description: e.Description,
			Source:      string(e.Source),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (d *toolsDialogDeps) ServerTools(ctx context.Context) []toolsdialog.ServerGroup {
	if d.app.runtime == nil || d.app.runtime.Catalog == nil {
		return nil
	}
	inv := d.app.runtime.Catalog.MCPManager().ToolInventory(ctx)
	out := make([]toolsdialog.ServerGroup, 0, len(inv))
	for _, g := range inv {
		group := toolsdialog.ServerGroup{Server: g.Server, Status: string(g.Status), Err: g.Err}
		for _, t := range g.Tools {
			group.Tools = append(group.Tools, toolsdialog.ServerTool{
				Name:        t.Name,
				WireName:    t.WireName,
				Description: t.Description,
				Enabled:     t.Enabled,
			})
		}
		out = append(out, group)
	}
	return out
}

func (d *toolsDialogDeps) SetToolEnabled(ctx context.Context, server, tool string, enabled bool) error {
	if d.app.deps.Loader == nil || d.app.deps.Settings == nil {
		return fmt.Errorf("settings not loaded")
	}
	servers, err := d.app.deps.Settings.MCPServers()
	if err != nil {
		return err
	}
	cfg, ok := servers[server]
	if !ok {
		return fmt.Errorf("server %q is not settings-managed; edit its source to change tools", server)
	}
	include, exclude := toggleToolFilter(cfg.IncludeTools, cfg.ExcludeTools, tool, enabled)
	if err := d.app.deps.Settings.SetMCPServerToolFilter(server, include, exclude); err != nil {
		return err
	}
	if err := d.app.deps.Loader.Save(d.app.deps.Settings); err != nil {
		return err
	}
	// A filter toggle changes only which discovered tools are exposed; the MCP
	// connections are unchanged, so rebuild the registry from the cached tool
	// set instead of forcing a full reconnect.
	return d.app.rebuildToolRegistry()
}

func (d *toolsDialogDeps) ReloadTools(ctx context.Context) error {
	_, err := d.app.deps.Hooks.ReloadMCP(ctx)
	return err
}

// toggleToolFilter returns updated include/exclude lists after enabling or
// disabling one tool. When an include allowlist is active, edits target the
// allowlist; otherwise edits target the exclude blocklist (default-all-on).
func toggleToolFilter(include, exclude []string, tool string, enabled bool) (newInclude, newExclude []string) {
	inc := toStringSet(include)
	exc := toStringSet(exclude)
	if len(inc) > 0 {
		if enabled {
			inc[tool] = struct{}{}
			delete(exc, tool)
		} else {
			delete(inc, tool)
		}
	} else {
		if enabled {
			delete(exc, tool)
		} else {
			exc[tool] = struct{}{}
		}
	}
	return setToSlice(inc), setToSlice(exc)
}

func toStringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func setToSlice(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	return out
}
