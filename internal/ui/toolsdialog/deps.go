// Package toolsdialog implements the /tools inventory overlay. It shows
// code-defined built-in tools (read-only) and MCP tools grouped by server, with
// per-tool enable/disable for MCP tools only. All side effects go through Deps
// so the dialog never imports the agent or slash packages (preserves AD-004).
package toolsdialog

import "context"

// BuiltinTool is a code-defined Sagittarius tool. These are listed but never
// editable or removable.
type BuiltinTool struct {
	Name        string
	Description string
	// Source is "builtin" or "skill" for display purposes.
	Source string
}

// ServerTool is one MCP tool with its current enabled state.
type ServerTool struct {
	Name        string // native tool name reported by the server
	WireName    string // qualified mcp_{server}_{tool} name
	Description string
	Enabled     bool
	// ReadOnly reports that the tool may run in ask mode, plan mode, and the
	// /readonly posture.
	ReadOnly bool
	// ReadOnlyFromHint distinguishes a tool admitted by the server's own
	// readOnlyHint annotation from one the user listed explicitly. Only the
	// latter is editable here; the former is the server's declaration.
	ReadOnlyFromHint bool
}

// ServerGroup groups an MCP server's tools with its connection status.
type ServerGroup struct {
	Server string
	Status string // connection status text (e.g. "connected")
	Err    string // discovery error, when present
	Tools  []ServerTool
}

// Deps performs the side effects the tool inventory needs.
type Deps interface {
	// BuiltinTools returns code-defined tools in registration order.
	BuiltinTools() []BuiltinTool
	// ServerTools returns MCP tools grouped by server (queried on demand).
	ServerTools(ctx context.Context) []ServerGroup
	// SetToolEnabled enables or disables one MCP tool on a server, persisting the
	// include/exclude filter and reloading the runner registry.
	SetToolEnabled(ctx context.Context, server, tool string, enabled bool) error
	// SetToolReadOnly adds or removes one MCP tool from its server's
	// readOnlyTools allowlist, persisting the change and reloading the registry.
	SetToolReadOnly(ctx context.Context, server, tool string, readOnly bool) error
	// ReloadTools re-discovers MCP tools and refreshes the registry.
	ReloadTools(ctx context.Context) error
}
