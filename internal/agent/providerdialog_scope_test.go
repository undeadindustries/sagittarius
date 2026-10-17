package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// scopeTestInactive is a built-in provider that is not active in scopeTestApp,
// so writes to it skip the runner rebuild (the test App has no hooks).
const scopeTestInactive = string(config.BuiltInOpenAI)

func TestProviderDialogApplySettingRoutesByScope(t *testing.T) {
	tests := []struct {
		name        string
		key, value  string
		scope       config.SettingScope
		wantProject bool
	}{
		{name: "preference in project scope", key: "temperature", value: "0.3", scope: config.ScopeProject, wantProject: true},
		{name: "preference in global scope", key: "temperature", value: "0.4", scope: config.ScopeGlobal},
		{name: "connection field forced global", key: "baseUrl", value: "http://127.0.0.1:9000/v1", scope: config.ScopeProject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := scopeTestApp(t)
			d := &providerDialogDeps{app: app}
			if err := d.ApplySetting(t.Context(), scopeTestInactive, tt.key, tt.value, tt.scope); err != nil {
				t.Fatalf("ApplySetting: %v", err)
			}
			project := d.ProviderSettings(scopeTestInactive, config.ScopeProject)[tt.key]
			global := d.ProviderSettings(scopeTestInactive, config.ScopeGlobal)[tt.key]
			if tt.wantProject {
				if project != tt.value || global != "" {
					t.Errorf("project=%q global=%q, want only project=%q", project, global, tt.value)
				}
				return
			}
			if global != tt.value {
				t.Errorf("global=%q, want %q", global, tt.value)
			}
			if tt.key != "baseUrl" && project != "" {
				t.Errorf("project=%q, want unset", project)
			}
		})
	}
}

func TestModelsDialogSettingScopes(t *testing.T) {
	app, _ := scopeTestApp(t)
	d := &modelsDialogDeps{app: app}
	const model = "gpt-4o"
	if err := d.SetModelSetting(t.Context(), scopeTestInactive, model, "temperature", "0.2", config.ScopeProject); err != nil {
		t.Fatalf("SetModelSetting(project): %v", err)
	}
	if got := d.GetModelSettings(scopeTestInactive, model, config.ScopeProject)["temperature"]; got != "0.2" {
		t.Errorf("project temperature = %q, want 0.2", got)
	}
	if got := d.GetModelSettings(scopeTestInactive, model, config.ScopeGlobal)["temperature"]; got != "" {
		t.Errorf("global temperature = %q, want unset", got)
	}
	if err := d.ClearModelSetting(t.Context(), scopeTestInactive, model, "temperature", config.ScopeProject); err != nil {
		t.Fatalf("ClearModelSetting(project): %v", err)
	}
	if got := d.GetModelSettings(scopeTestInactive, model, config.ScopeProject)["temperature"]; got != "" {
		t.Errorf("project temperature after clear = %q, want unset", got)
	}
}
