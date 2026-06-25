// Package mcpdialog implements the /mcp server management overlay: a menu-first
// wizard to list, add, edit, enable/disable, and remove MCP servers configured
// in settings.json. Extension-sourced servers are shown read-only. All side
// effects go through Deps so the dialog never imports the agent or slash
// packages (preserves AD-004).
package mcpdialog

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// Transport identifiers for the form transport toggle.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
	TransportSSE   = "sse"
)

// ServerEntry is a row in the server list.
type ServerEntry struct {
	Name      string
	Transport string
	Detail    string // command or URL summary
	Status    string // connection status text
	ToolCount int
	Disabled  bool
	// Editable is false for extension-sourced servers, which can be viewed but
	// not edited or removed.
	Editable bool
	// Source is "settings" or an extension descriptor for display.
	Source string
	// Scope indicates whether this server is defined in global or project settings.
	// Only meaningful when Editable is true (Source == "settings").
	Scope config.SettingScope
}

// ServerForm holds the editable string/bool fields for one server. Args, Env,
// and Headers use compact text encodings the adapter parses on save:
//   - Args:    space-separated tokens
//   - Env:     comma-separated K=V pairs
//   - Headers: comma-separated K=V pairs
//
// Bearer is write-only: it is never read back into the form and, when set, is
// stored in the credentials layer rather than settings.json.
type ServerForm struct {
	Name        string
	Transport   string
	Command     string
	Args        string
	URL         string
	Env         string
	Headers     string
	Bearer      string
	Timeout     string
	Description string
	Trust       bool
	Disabled    bool
}

// Deps performs the settings and credential side effects the wizard needs.
type Deps interface {
	// ListServers returns every configured server (settings + extension).
	ListServers() []ServerEntry
	// GetServer returns the editable form for a server, or ok=false if absent.
	GetServer(name string) (ServerForm, bool)
	// SaveServer adds or replaces a server into the specified scope.
	// originalName is empty when adding; on edit it identifies the existing
	// entry. Persists settings, stores any bearer token, and reloads.
	SaveServer(ctx context.Context, originalName string, form ServerForm, scope config.SettingScope) error
	// RemoveServer deletes a settings-owned server. The implementation
	// auto-detects which scope owns the server and removes it there.
	RemoveServer(ctx context.Context, name string) error
	// SetDisabled toggles a server's disabled flag in the scope that owns it.
	SetDisabled(ctx context.Context, name string, disabled bool) error
	// Reload reconnects servers and rediscovers tools, returning a status line.
	Reload(ctx context.Context) (string, error)
	// ProjectAvailable reports whether the project scope is writable.
	ProjectAvailable() bool
}
