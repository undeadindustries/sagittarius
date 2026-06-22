package tools

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Registry holds built-in tools indexed by wire name.
type Registry struct {
	byName  map[string]Tool
	order   []Tool
	aliases map[string]string
}

// NewBuiltinRegistry registers all core built-in tools for a workspace.
func NewBuiltinRegistry(ws *Workspace) *Registry {
	r := &Registry{
		byName:  make(map[string]Tool),
		aliases: copyAliases(),
	}
	for _, tool := range []Tool{
		newReadFileTool(ws),
		newWriteFileTool(ws),
		newListDirectoryTool(ws),
		newShellTool(ws),
		newGrepTool(ws),
	} {
		r.Register(tool)
	}
	return r
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool Tool) {
	name := tool.Name()
	r.byName[name] = tool
	r.order = append(r.order, tool)
}

// Lookup resolves a tool by wire name or legacy alias.
func (r *Registry) Lookup(name string) (Tool, bool) {
	if tool, ok := r.byName[name]; ok {
		return tool, true
	}
	if canonical, ok := r.aliases[name]; ok {
		tool, ok := r.byName[canonical]
		return tool, ok
	}
	return nil, false
}

// ListDeclarations returns tool schemas for provider.GenerateRequest.Tools.
func (r *Registry) ListDeclarations() []provider.ToolDeclaration {
	out := make([]provider.ToolDeclaration, 0, len(r.order))
	for _, tool := range r.order {
		out = append(out, tool.Declaration())
	}
	return out
}

// ListDeclarationsForMode returns tool schemas visible to the model for the
// given interaction mode (plan/ask hide write and shell tools).
func (r *Registry) ListDeclarationsForMode(mode modes.Mode) []provider.ToolDeclaration {
	if mode == modes.ModeAgent || mode == modes.ModeDebug {
		return r.ListDeclarations()
	}
	out := make([]provider.ToolDeclaration, 0, len(r.order))
	for _, tool := range r.order {
		name := tool.Name()
		if ToolVisibleInMode(mode, name) {
			out = append(out, tool.Declaration())
		}
	}
	return out
}

func copyAliases() map[string]string {
	out := make(map[string]string, len(legacyAliases))
	for k, v := range legacyAliases {
		out[k] = v
	}
	return out
}

func stringArg(args map[string]any, key string) (string, error) {
	raw, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter %q", key)
	}
	s, ok := raw.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("parameter %q must be a non-empty string", key)
	}
	return s, nil
}

func optionalStringArg(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return s
}

func intArg(args map[string]any, key string) (int, bool, error) {
	raw, ok := args[key]
	if !ok {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case int:
		return v, true, nil
	case int64:
		return int(v), true, nil
	case float64:
		return int(v), true, nil
	default:
		return 0, false, fmt.Errorf("parameter %q must be an integer", key)
	}
}

func boolArg(args map[string]any, key string) (bool, bool, error) {
	raw, ok := args[key]
	if !ok {
		return false, false, nil
	}
	switch v := raw.(type) {
	case bool:
		return v, true, nil
	default:
		return false, false, fmt.Errorf("parameter %q must be a boolean", key)
	}
}
