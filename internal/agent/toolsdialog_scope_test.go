package agent

import (
	"slices"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestToolToggleWritesDefiningScope: a tool filter is saved on the server
// entry in whichever file defines it. Writing a project server's filter to
// global would be shadowed by the project entry and silently do nothing.
func TestToolToggleWritesDefiningScope(t *testing.T) {
	tests := []struct {
		name      string
		scope     config.SettingScope
		wantScope config.SettingScope
	}{
		{name: "project server", scope: config.ScopeProject, wantScope: config.ScopeProject},
		{name: "global server", scope: config.ScopeGlobal, wantScope: config.ScopeGlobal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := scopeTestApp(t)
			if err := app.persistScope(tt.scope, func(s *config.Settings) error {
				return s.SetMCPServer("docs", config.MCPServerConfig{Command: "docs-mcp"})
			}); err != nil {
				t.Fatalf("define server: %v", err)
			}
			if got := app.docs.MCPServerScope("docs"); got != tt.wantScope {
				t.Fatalf("MCPServerScope = %v, want %v", got, tt.wantScope)
			}

			d := &toolsDialogDeps{app: app}
			// The test App has no runtime, so the registry rebuild after the
			// save reports "runtime not available"; only the save is under test.
			_ = d.SetToolEnabled(t.Context(), "docs", "search", false)

			servers, err := app.docs.TargetSettings(tt.wantScope).MCPServers()
			if err != nil {
				t.Fatalf("MCPServers: %v", err)
			}
			if !slices.Contains(servers["docs"].ExcludeTools, "search") {
				t.Errorf("%v docs server excludeTools = %v, want search", tt.wantScope, servers["docs"].ExcludeTools)
			}
		})
	}
}
