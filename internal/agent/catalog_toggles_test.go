package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

func newToggleCatalog(t *testing.T, cfg CatalogConfig) *Catalog {
	// Tests explicitly set what they care about, but default edit to true
	// because it defaults to true in settings.
	cfg.EditEnabled = true
	t.Helper()
	ws, err := tools.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cfg.Workspace = ws
	cat, err := NewCatalog(cfg)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })
	return cat
}

func settingsWithSymbolsEnabled(enabled bool) *config.Settings {
	return &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols: &config.SagittariusSymbolsConfig{Enabled: &enabled},
		},
	}
}

// TestRefreshBuiltinTogglesAppliesSymbolsChange is the regression test for the
// /settings toggle that persisted and rebuilt the runner but left find_symbol
// registered until a full restart.
func TestRefreshBuiltinTogglesAppliesSymbolsChange(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: true, SymbolsPreferGopls: true})

	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Fatalf("%s should be registered at startup", tools.FindSymbolToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithSymbolsEnabled(false)); !changed {
		t.Fatal("disabling symbols should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); ok {
		t.Errorf("%s should be gone after the toggle is disabled", tools.FindSymbolToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithSymbolsEnabled(true)); !changed {
		t.Fatal("re-enabling symbols should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Errorf("%s should return after the toggle is re-enabled", tools.FindSymbolToolName)
	}
}

func settingsWithEditEnabled(enabled bool) *config.Settings {
	return &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Edit: &config.SagittariusEditConfig{Enabled: &enabled},
		},
	}
}

func TestRefreshBuiltinTogglesEditTool(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{EditEnabled: true})

	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); !ok {
		t.Fatalf("%s should be registered at startup", tools.EditToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithEditEnabled(false)); !changed {
		t.Fatal("disabling edit should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); ok {
		t.Errorf("%s should be gone after the toggle is disabled", tools.EditToolName)
	}

	if changed := cat.RefreshBuiltinToggles(settingsWithEditEnabled(true)); !changed {
		t.Fatal("re-enabling edit should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.EditToolName); !ok {
		t.Errorf("%s should return after the toggle is re-enabled", tools.EditToolName)
	}
}

// TestRefreshBuiltinTogglesNoopWhenUnchanged keeps mode switches and model picks
// free of registry churn: RebuildRunner only reinstalls a registry on a change.
// Every toggle is set explicitly so the result cannot depend on ambient
// credentials or on a default changing underneath the test.
func TestRefreshBuiltinTogglesNoopWhenUnchanged(t *testing.T) {
	off, on := false, true
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: true, SymbolsPreferGopls: true, EditEnabled: true, ResearchSubagentsEnabled: false, CodingSubagentsEnabled: false, ScratchpadEnabled: true})

	same := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols:           &config.SagittariusSymbolsConfig{Enabled: &on, PreferGopls: &on},
			Web:               &config.SagittariusWebConfig{SearchEnabled: &off, FetchEnabled: &off},
			ScratchpadEnabled: &on,
		},
	}
	if changed := cat.RefreshBuiltinToggles(same); changed {
		t.Fatal("settings equal to the current state should report no change")
	}
	if changed := cat.RefreshBuiltinToggles(same); changed {
		t.Error("a second identical refresh should still report no change")
	}
}

// TestRefreshBuiltinTogglesSettingsWinOverStartupFlags documents the precedence:
// the CatalogConfig flags seed the initial state, after which the live settings
// document is authoritative. main.go derives those flags from the same resolvers,
// so the two agree at startup in production.
func TestRefreshBuiltinTogglesSettingsWinOverStartupFlags(t *testing.T) {
	enabled := true
	cat := newToggleCatalog(t, CatalogConfig{SymbolsEnabled: false})

	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); ok {
		t.Fatalf("%s should be absent when constructed disabled", tools.FindSymbolToolName)
	}
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Symbols: &config.SagittariusSymbolsConfig{Enabled: &enabled},
		},
	}); !changed {
		t.Fatal("settings enabling symbols should override the startup flag")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.FindSymbolToolName); !ok {
		t.Errorf("%s should be present once settings enable it", tools.FindSymbolToolName)
	}
}

// TestRefreshBuiltinTogglesResolvesFetchBudget asserts sagittarius.web
// .maxFetchBytes reaches the registry rather than defaulting to a zero budget.
func TestRefreshBuiltinTogglesScriptTool(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{})

	if _, ok := cat.BuildRegistry().Lookup(tools.ScriptToolName); ok {
		t.Fatalf("%s should be off by default", tools.ScriptToolName)
	}

	on := true
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{ScriptToolEnabled: &on},
	}); !changed {
		t.Fatal("enabling run_script should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.ScriptToolName); !ok {
		t.Errorf("%s should register after the toggle", tools.ScriptToolName)
	}

	off := false
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{ScriptToolEnabled: &off},
	}); !changed {
		t.Fatal("disabling run_script should report a change")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.ScriptToolName); ok {
		t.Errorf("%s should be gone after disable", tools.ScriptToolName)
	}
}

// TestRefreshBuiltinTogglesScratchpad guards the AD-085 dead-toggle class: the
// update_scratchpad tool is registered by the runner (it needs a *Runner), so
// the catalog tracks the setting purely to make a live /settings change report
// a difference. Without that, the toggle would persist and rebuild the runner
// while the tool stayed registered until a restart.
func TestRefreshBuiltinTogglesScratchpad(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{ScratchpadEnabled: true})

	off, on := false, true
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{ScratchpadEnabled: &off},
	}); !changed {
		t.Fatal("disabling the scratchpad should report a change so the registry rebuilds")
	}
	if cat.scratchpadEnabled {
		t.Error("catalog still reports the scratchpad enabled after the toggle")
	}

	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{ScratchpadEnabled: &on},
	}); !changed {
		t.Fatal("re-enabling the scratchpad should report a change")
	}
	if !cat.scratchpadEnabled {
		t.Error("catalog does not report the scratchpad enabled after re-enabling")
	}
}

func TestRefreshBuiltinTogglesSkills(t *testing.T) {
	cat := newToggleCatalog(t, CatalogConfig{})
	if !cat.SkillsEnabled() {
		t.Fatal("skills should be enabled by default")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.ActivateSkillToolName); !ok {
		t.Fatal("activate_skill should be registered by default")
	}

	off, on := false, true
	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{SkillsEnabled: &off},
	}); !changed {
		t.Fatal("disabling skills should report a change so the registry rebuilds")
	}
	if cat.SkillsEnabled() {
		t.Error("catalog still reports skills enabled after toggle")
	}
	if cat.SkillManager().Enabled() {
		t.Error("skill manager still reports enabled after toggle")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.ActivateSkillToolName); ok {
		t.Error("activate_skill was registered with skillsEnabled=false")
	}

	if changed := cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{SkillsEnabled: &on},
	}); !changed {
		t.Fatal("re-enabling skills should report a change")
	}
	if !cat.SkillsEnabled() || !cat.SkillManager().Enabled() {
		t.Error("skills did not re-enable")
	}
	if _, ok := cat.BuildRegistry().Lookup(tools.ActivateSkillToolName); !ok {
		t.Error("activate_skill missing after re-enabling skills")
	}
}

// TestSetRegistryHonorsLiveScratchpadSetting closes the other half of the loop:
// refreshBuiltinToolToggles rebuilds and calls SetRegistry, which must consult
// the live settings snapshot rather than whatever was true at construction.
func TestSetRegistryHonorsLiveScratchpadSetting(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if _, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName); !ok {
		t.Fatal("update_scratchpad should be registered by default")
	}

	ws, err := tools.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	off := false
	runner.SetSettings(&config.Settings{
		Sagittarius: &config.SagittariusSettings{ScratchpadEnabled: &off},
	})
	runner.SetRegistry(tools.NewBuiltinRegistry(ws))

	if _, ok := runner.Registry().Lookup(tools.UpdateScratchpadToolName); ok {
		t.Error("update_scratchpad survived a rebuild with scratchpadEnabled=false")
	}
}

func TestRefreshBuiltinTogglesResolvesFetchBudget(t *testing.T) {
	// Web tools stay off so no Gemini utility client (and no credential lookup)
	// is attempted; the budget field is resolved either way.
	cat := newToggleCatalog(t, CatalogConfig{})

	if cat.webMaxFetchBytes != config.DefaultMaxFetchBytes {
		t.Errorf("startup budget = %d; want the default %d", cat.webMaxFetchBytes, config.DefaultMaxFetchBytes)
	}

	budget := 4096
	cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Web: &config.SagittariusWebConfig{MaxFetchBytes: &budget},
		},
	})
	if cat.webMaxFetchBytes != budget {
		t.Errorf("configured budget = %d; want %d", cat.webMaxFetchBytes, budget)
	}
}

// TestRunnerSystemPromptGatesSkillsSection verifies that the ## Skills section
// is omitted when the catalog has no skills and appears once a skill is
// discovered and the registry is rebuilt.
func TestRunnerSystemPromptGatesSkillsSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	workDir := t.TempDir()
	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	skillMgr := skills.NewManager(workDir, true)
	if err := skillMgr.Discover(t.Context(), nil); err != nil {
		t.Fatalf("Discover (empty): %v", err)
	}
	cat, err := NewCatalog(CatalogConfig{
		Workspace:   ws,
		Skills:      skillMgr,
		EditEnabled: true,
	})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	t.Cleanup(func() { _ = cat.Close() })

	rt := &Runtime{Catalog: cat}
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     workDir,
		Interactive: false,
		Runtime:     rt,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	runner.SetRegistry(cat.BuildRegistry())

	if sys := systemInstruction(t, runner); strings.Contains(sys, "## Skills") {
		t.Fatalf("system prompt must not contain ## Skills when zero skills are installed:\n%s", sys)
	}

	skillDir := filepath.Join(workDir, ".sagittarius", "skills", "python3")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const skillMD = "---\nname: python3\ndescription: Python engineering standards\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := rt.ReloadSkills(t.Context())
	if err != nil {
		t.Fatalf("ReloadSkills: %v", err)
	}
	runner.SetRegistry(reg)

	if sys := systemInstruction(t, runner); !strings.Contains(sys, "## Skills") {
		t.Fatalf("system prompt should contain ## Skills after discovering a skill:\n%s", sys)
	}

	// Disabling skills via settings drops activate_skill from the rebuilt
	// registry, strips ## Skills from the prompt, empties SkillNames, and
	// refuses @skill: mentions.
	off := false
	cat.RefreshBuiltinToggles(&config.Settings{
		Sagittarius: &config.SagittariusSettings{SkillsEnabled: &off},
	})
	runner.SetRegistry(cat.BuildRegistry())

	if _, ok := runner.Registry().Lookup(tools.ActivateSkillToolName); ok {
		t.Fatal("activate_skill remained in registry when skillsEnabled=false")
	}
	if sys := systemInstruction(t, runner); strings.Contains(sys, "## Skills") || strings.Contains(sys, tools.ActivateSkillToolName) {
		t.Fatalf("system prompt still advertises skills when skillsEnabled=false:\n%s", sys)
	}
	if got := runner.SkillNames(); len(got) != 0 {
		t.Fatalf("SkillNames when disabled = %v, want empty", got)
	}
	events := collectEvents(t, mustRunTurn(t, runner, "@skill:python3 go"))
	var sawDisabled bool
	for _, ev := range events {
		if ev.Err != nil && strings.Contains(ev.Err.Error(), "skills are turned off") {
			sawDisabled = true
		}
	}
	if !sawDisabled {
		t.Fatalf("expected skills-turned-off error for @skill:python3, got %+v", events)
	}
}
