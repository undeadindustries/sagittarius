package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helpers ------------------------------------------------------------------

func mustEncodeRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// LoadDocuments tests -------------------------------------------------------

func TestLoadDocuments_NoProjectFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	workDir := t.TempDir()
	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	if docs.Global == nil {
		t.Fatal("Global must not be nil")
	}
	if docs.Project != nil {
		t.Fatal("Project must be nil when no file exists")
	}
	if docs.Merged == nil {
		t.Fatal("Merged must not be nil")
	}
}

func TestLoadDocuments_WithProjectFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	workDir := t.TempDir()
	projectDir := filepath.Join(workDir, ".sagittarius")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	projectSettings := `{"sagittarius":{"systemPrompt":{"personality":"sysadmin","variant":"lite"}}}`
	if err := os.WriteFile(filepath.Join(projectDir, "settings.json"), []byte(projectSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	if docs.Project == nil {
		t.Fatal("Project must not be nil when file exists")
	}
	// Merged should have the project personality.
	if docs.Merged.Sagittarius == nil || docs.Merged.Sagittarius.SystemPrompt == nil {
		t.Fatal("Merged.Sagittarius.SystemPrompt must not be nil")
	}
	if got := docs.Merged.Sagittarius.SystemPrompt.Personality; got != "sysadmin" {
		t.Fatalf("Merged personality = %q, want %q", got, "sysadmin")
	}
	if got := docs.Merged.Sagittarius.SystemPrompt.Variant; got != "lite" {
		t.Fatalf("Merged variant = %q, want %q", got, "lite")
	}
}

// mergeSettings precedence -------------------------------------------------

func TestMergeSettings_ProjectWinsScalar(t *testing.T) {
	global := &Settings{
		Sagittarius: &SagittariusSettings{DefaultModel: "global-model"},
		Raw:         map[string]json.RawMessage{},
	}
	project := &Settings{
		Sagittarius: &SagittariusSettings{DefaultModel: "project-model"},
		Raw:         map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, project)
	if got := merged.Sagittarius.DefaultModel; got != "project-model" {
		t.Fatalf("DefaultModel = %q, want project-model", got)
	}
}

func TestMergeSettings_GlobalUsedWhenProjectEmpty(t *testing.T) {
	global := &Settings{
		Sagittarius: &SagittariusSettings{DefaultModel: "global-model"},
		Raw:         map[string]json.RawMessage{},
	}
	project := &Settings{
		Sagittarius: &SagittariusSettings{}, // DefaultModel empty
		Raw:         map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, project)
	if got := merged.Sagittarius.DefaultModel; got != "global-model" {
		t.Fatalf("DefaultModel = %q, want global-model", got)
	}
}

func TestMergeSettings_NilProject(t *testing.T) {
	global := &Settings{
		Sagittarius: &SagittariusSettings{DefaultModel: "global-model"},
		Raw:         map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, nil)
	if merged != global {
		t.Fatal("mergeSettings with nil project must return global unchanged")
	}
}

func TestMergeSettings_ModeOverride(t *testing.T) {
	global := &Settings{
		Sagittarius: &SagittariusSettings{
			Modes: &SagittariusModes{
				Plan: &SagittariusModeConfig{Model: "global-plan-model"},
			},
		},
		Raw: map[string]json.RawMessage{},
	}
	project := &Settings{
		Sagittarius: &SagittariusSettings{
			Modes: &SagittariusModes{
				Plan: &SagittariusModeConfig{Model: "project-plan-model"},
			},
		},
		Raw: map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, project)
	if got := merged.Sagittarius.Modes.Plan.Model; got != "project-plan-model" {
		t.Fatalf("Plan.Model = %q, want project-plan-model", got)
	}
}

func TestMergeSettings_SecurityBoundary(t *testing.T) {
	global := &Settings{
		Security: &SecuritySettings{
			ProjectBoundary: &ProjectBoundaryConfig{Enforce: boolPtr(false)},
		},
		Raw: map[string]json.RawMessage{},
	}
	project := &Settings{
		Security: &SecuritySettings{
			ProjectBoundary: &ProjectBoundaryConfig{Enforce: boolPtr(true)},
		},
		Raw: map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, project)
	if !ProjectBoundaryEnforced(merged, nil) {
		t.Fatal("expected project boundary enforced = true after merge")
	}
}

func TestMergeSettings_SnapshotMerge(t *testing.T) {
	global := &Settings{
		Sagittarius: &SagittariusSettings{
			Snapshots: &SagittariusSnapshotConfig{Enabled: boolPtr(true), MaxFileBytes: intPtr(1024)},
		},
		Raw: map[string]json.RawMessage{},
	}
	project := &Settings{
		Sagittarius: &SagittariusSettings{
			Snapshots: &SagittariusSnapshotConfig{MaxFileBytes: intPtr(2048)},
		},
		Raw: map[string]json.RawMessage{},
	}
	merged := mergeSettings(global, project)
	if !SnapshotsEnabled(merged, nil) {
		t.Fatal("expected snapshots enabled (inherited from global)")
	}
	if got := SnapshotMaxFileBytes(merged, nil); got != 2048 {
		t.Fatalf("MaxFileBytes = %d, want 2048", got)
	}
}

// MCP shallow merge --------------------------------------------------------

func TestMergeMCPServersRaw(t *testing.T) {
	globalRaw := mustEncodeRaw(t, map[string]any{
		"serverA": map[string]any{"command": "a"},
		"serverB": map[string]any{"command": "b"},
	})
	projectRaw := mustEncodeRaw(t, map[string]any{
		"serverB": map[string]any{"command": "b-override"},
		"serverC": map[string]any{"command": "c"},
	})

	mergedRaw := mergeMCPServersRaw(globalRaw, projectRaw)
	var result map[string]map[string]any
	if err := json.Unmarshal(mergedRaw, &result); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}

	// serverA from global
	if cmd := result["serverA"]["command"]; cmd != "a" {
		t.Fatalf("serverA.command = %v, want a", cmd)
	}
	// serverB overridden by project
	if cmd := result["serverB"]["command"]; cmd != "b-override" {
		t.Fatalf("serverB.command = %v, want b-override", cmd)
	}
	// serverC added by project
	if cmd := result["serverC"]["command"]; cmd != "c" {
		t.Fatalf("serverC.command = %v, want c", cmd)
	}
}

func TestMergeRaw_MCPServersShallowMerge(t *testing.T) {
	global := map[string]json.RawMessage{
		"mcpServers": mustEncodeRaw(t, map[string]any{
			"alpha": map[string]any{"command": "alpha-global"},
		}),
		"ui": mustEncodeRaw(t, map[string]any{"theme": "default"}),
	}
	project := map[string]json.RawMessage{
		"mcpServers": mustEncodeRaw(t, map[string]any{
			"beta": map[string]any{"command": "beta-project"},
		}),
	}

	merged := mergeRaw(global, project)

	var mcpResult map[string]map[string]any
	if err := json.Unmarshal(merged["mcpServers"], &mcpResult); err != nil {
		t.Fatalf("unmarshal mcpServers: %v", err)
	}
	if _, ok := mcpResult["alpha"]; !ok {
		t.Fatal("alpha server should survive shallow merge")
	}
	if _, ok := mcpResult["beta"]; !ok {
		t.Fatal("beta server from project should appear in merged")
	}

	// ui from global should still be present
	var uiResult map[string]any
	if err := json.Unmarshal(merged["ui"], &uiResult); err != nil {
		t.Fatalf("unmarshal ui: %v", err)
	}
	if uiResult["theme"] != "default" {
		t.Fatalf("ui.theme = %v, want default", uiResult["theme"])
	}
}

// activeModels replacement -------------------------------------------------

func TestMergeProviderInstance_ActiveModelsReplacement(t *testing.T) {
	global := &ProviderInstanceConfig{ActiveModels: []string{"modelA", "modelB"}}
	project := &ProviderInstanceConfig{ActiveModels: []string{"modelC"}}
	merged := mergeProviderInstance(global, project)
	if len(merged.ActiveModels) != 1 || merged.ActiveModels[0] != "modelC" {
		t.Fatalf("ActiveModels = %v, want [modelC]", merged.ActiveModels)
	}
}

func TestMergeProviderInstance_ModelsShallowMerge(t *testing.T) {
	global := &ProviderInstanceConfig{
		Models: map[string]ProviderModelConfig{
			"m1": {Personality: "programmer"},
			"m2": {Personality: "sysadmin"},
		},
	}
	project := &ProviderInstanceConfig{
		Models: map[string]ProviderModelConfig{
			"m2": {Personality: "creative-assistant"},
			"m3": {Personality: "personal-assistant"},
		},
	}
	merged := mergeProviderInstance(global, project)
	if got := merged.Models["m1"].Personality; got != "programmer" {
		t.Fatalf("m1.Personality = %q, want programmer", got)
	}
	if got := merged.Models["m2"].Personality; got != "creative-assistant" {
		t.Fatalf("m2.Personality = %q, want creative-assistant (project wins)", got)
	}
	if got := merged.Models["m3"].Personality; got != "personal-assistant" {
		t.Fatalf("m3.Personality = %q, want personal-assistant", got)
	}
}

// IsDefined / ScopeOf ------------------------------------------------------

func TestDocuments_IsDefined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	workDir := t.TempDir()
	projectDir := filepath.Join(workDir, ".sagittarius")
	if err := os.MkdirAll(projectDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "settings.json"),
		[]byte(`{"sagittarius":{"modes":{"agent":{"model":"proj-model"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}

	// sagittarius is defined in project
	if !docs.IsDefined(ScopeProject, "sagittarius") {
		t.Fatal("expected sagittarius to be defined in project scope")
	}
	// providers is not in project
	if docs.IsDefined(ScopeProject, "providers") {
		t.Fatal("expected providers to be absent in project scope")
	}
	// ScopeOf should return ScopeProject for sagittarius
	if got := docs.ScopeOf("sagittarius"); got != ScopeProject {
		t.Fatalf("ScopeOf(sagittarius) = %v, want ScopeProject", got)
	}
}

// SaveProject --------------------------------------------------------------

func TestDocuments_SaveProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	workDir := t.TempDir()

	docs, err := LoadDocuments(workDir)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}

	// Set a system prompt in project scope and save.
	docs.Project = &Settings{
		Sagittarius: &SagittariusSettings{
			SystemPrompt: &SagittariusSystemPromptConfig{Personality: "sysadmin", Variant: "lite"},
		},
		Raw: map[string]json.RawMessage{},
	}
	if err := docs.SaveProject(); err != nil {
		t.Fatalf("SaveProject: %v", err)
	}

	// Reload from disk and verify.
	loaded, err := LoadProjectSettings(workDir)
	if err != nil {
		t.Fatalf("LoadProjectSettings: %v", err)
	}
	if loaded == nil || loaded.Sagittarius == nil || loaded.Sagittarius.SystemPrompt == nil {
		t.Fatal("project settings not persisted")
	}
	if got := loaded.Sagittarius.SystemPrompt.Personality; got != "sysadmin" {
		t.Fatalf("persisted personality = %q, want sysadmin", got)
	}

	// Merged should reflect the new project value.
	if docs.Merged == nil || docs.Merged.Sagittarius == nil ||
		docs.Merged.Sagittarius.SystemPrompt == nil ||
		docs.Merged.Sagittarius.SystemPrompt.Personality != "sysadmin" {
		t.Fatal("Merged not updated after SaveProject")
	}
}
