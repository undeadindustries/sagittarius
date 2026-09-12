package agent

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui/settingsdialog"
)

func TestApplySettingValueEditAndSubagents(t *testing.T) {
	s := &config.Settings{}

	// Test Edit toggle
	if err := applySettingValue(s, "sagittarius.edit.enabled", "true"); err != nil {
		t.Fatalf("apply edit.enabled: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.Edit == nil || s.Sagittarius.Edit.Enabled == nil || *s.Sagittarius.Edit.Enabled != true {
		t.Errorf("edit.enabled was not set to true")
	}

	// Clear Edit toggle
	if err := clearSettingValue(s, "sagittarius.edit.enabled"); err != nil {
		t.Fatalf("clear edit.enabled: %v", err)
	}
	if s.Sagittarius.Edit.Enabled != nil {
		t.Errorf("edit.enabled was not cleared")
	}

	// Test Subagents toggle
	if err := applySettingValue(s, "sagittarius.subagents.enabled", "true"); err != nil {
		t.Fatalf("apply subagents.enabled: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.Subagents == nil || s.Sagittarius.Subagents.Enabled == nil || *s.Sagittarius.Subagents.Enabled != true {
		t.Errorf("subagents.enabled was not set to true")
	}

	// Clear Subagents toggle
	if err := clearSettingValue(s, "sagittarius.subagents.enabled"); err != nil {
		t.Fatalf("clear subagents.enabled: %v", err)
	}
	if s.Sagittarius.Subagents.Enabled != nil {
		t.Errorf("subagents.enabled was not cleared")
	}
}

// The two classes are separate switches on purpose: enabling research must
// never hand the model a write-capable child, so each key has to land in its
// own block and clear independently.
func TestApplySettingValueSubagentClasses(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.subagents.research.enabled", "true"); err != nil {
		t.Fatalf("apply research.enabled: %v", err)
	}
	subs := s.Sagittarius.Subagents
	if subs.Research == nil || subs.Research.Enabled == nil || !*subs.Research.Enabled {
		t.Fatalf("research.enabled not set: %+v", subs.Research)
	}
	if subs.Coding != nil {
		t.Error("enabling research must not create a coding block")
	}
	if subs.Enabled != nil {
		t.Error("enabling research must not touch the legacy switch")
	}

	if err := applySettingValue(s, "sagittarius.subagents.coding.enabled", "true"); err != nil {
		t.Fatalf("apply coding.enabled: %v", err)
	}
	if subs.Coding == nil || subs.Coding.Enabled == nil || !*subs.Coding.Enabled {
		t.Fatalf("coding.enabled not set: %+v", subs.Coding)
	}

	if err := clearSettingValue(s, "sagittarius.subagents.coding.enabled"); err != nil {
		t.Fatalf("clear coding.enabled: %v", err)
	}
	if subs.Coding.Enabled != nil {
		t.Error("coding.enabled was not cleared")
	}
	if subs.Research.Enabled == nil || !*subs.Research.Enabled {
		t.Error("clearing coding cleared research too")
	}
}

// Clearing a class that was never set must not fabricate the block, or the row
// would report itself as defined in this scope afterwards (AD-129).
func TestClearSubagentClassOnEmptyDocument(t *testing.T) {
	s := &config.Settings{}
	if err := clearSettingValue(s, "sagittarius.subagents.coding.enabled"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if s.Sagittarius != nil && s.Sagittarius.Subagents != nil {
		t.Errorf("clear created a subagents block: %+v", s.Sagittarius.Subagents)
	}
}

func TestApplySettingValueDefaultMode(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.defaultMode", "plan"); err != nil {
		t.Fatalf("apply defaultMode: %v", err)
	}
	if s.Sagittarius == nil || s.Sagittarius.DefaultMode != "plan" {
		t.Errorf("defaultMode = %q, want plan", s.Sagittarius.DefaultMode)
	}

	if err := clearSettingValue(s, "sagittarius.defaultMode"); err != nil {
		t.Fatalf("clear defaultMode: %v", err)
	}
	if s.Sagittarius.DefaultMode != "" {
		t.Errorf("defaultMode was not cleared, got %q", s.Sagittarius.DefaultMode)
	}
}

func TestApplySettingValueGoal(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorModel", "qwen/qwen3.5-122b"); err != nil {
		t.Fatalf("apply evaluatorModel: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorModel != "qwen/qwen3.5-122b" {
		t.Fatalf("evaluatorModel = %q", s.Sagittarius.Goal.EvaluatorModel)
	}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorProvider", "openrouter"); err != nil {
		t.Fatalf("apply evaluatorProvider: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorProvider != "openrouter" {
		t.Fatalf("evaluatorProvider = %q", s.Sagittarius.Goal.EvaluatorProvider)
	}

	if err := applySettingValue(s, "sagittarius.goal.evaluatorTimeout", "180"); err != nil {
		t.Fatalf("apply evaluatorTimeout: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorTimeout == nil || *s.Sagittarius.Goal.EvaluatorTimeout != 180 {
		t.Fatalf("evaluatorTimeout = %v", s.Sagittarius.Goal.EvaluatorTimeout)
	}

	if err := applySettingValue(s, "sagittarius.goal.maxTurns", "10"); err != nil {
		t.Fatalf("apply maxTurns: %v", err)
	}
	if s.Sagittarius.Goal.MaxTurns == nil || *s.Sagittarius.Goal.MaxTurns != 10 {
		t.Fatalf("maxTurns = %v", s.Sagittarius.Goal.MaxTurns)
	}

	if err := clearSettingValue(s, "sagittarius.goal.evaluatorModel"); err != nil {
		t.Fatalf("clear evaluatorModel: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorModel != "" {
		t.Fatalf("evaluatorModel not cleared: %q", s.Sagittarius.Goal.EvaluatorModel)
	}
	if err := clearSettingValue(s, "sagittarius.goal.evaluatorTimeout"); err != nil {
		t.Fatalf("clear evaluatorTimeout: %v", err)
	}
	if s.Sagittarius.Goal.EvaluatorTimeout != nil {
		t.Fatal("evaluatorTimeout not cleared")
	}
}

func TestApplySettingValueMaxToolRounds(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "0"); err != nil {
		t.Fatalf("apply 0: %v", err)
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 0 {
		t.Fatalf("MaxToolRounds = %v, want 0", s.Sagittarius.MaxToolRounds)
	}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "250"); err != nil {
		t.Fatalf("apply 250: %v", err)
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 250 {
		t.Fatalf("MaxToolRounds = %v, want 250", s.Sagittarius.MaxToolRounds)
	}

	if err := applySettingValue(s, "sagittarius.maxToolRounds", "-1"); err == nil {
		t.Fatal("expected error for negative maxToolRounds")
	}
	if s.Sagittarius.MaxToolRounds == nil || *s.Sagittarius.MaxToolRounds != 250 {
		t.Fatalf("rejected value mutated MaxToolRounds to %v", s.Sagittarius.MaxToolRounds)
	}
}

func TestApplySettingValueMemoryMaxRunes(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.memory.maxRunes", "4096"); err != nil {
		t.Fatalf("apply 4096: %v", err)
	}
	if s.Sagittarius.Memory == nil || s.Sagittarius.Memory.MaxRunes == nil || *s.Sagittarius.Memory.MaxRunes != 4096 {
		t.Fatalf("Memory.MaxRunes = %v, want 4096", s.Sagittarius.Memory)
	}

	// 0 is unlimited, not invalid (the AD-122 maxToolRounds convention).
	if err := applySettingValue(s, "sagittarius.memory.maxRunes", "0"); err != nil {
		t.Fatalf("apply 0: %v", err)
	}
	if *s.Sagittarius.Memory.MaxRunes != 0 {
		t.Fatalf("Memory.MaxRunes = %v, want 0", *s.Sagittarius.Memory.MaxRunes)
	}

	if err := applySettingValue(s, "sagittarius.memory.maxRunes", "-1"); err == nil {
		t.Fatal("expected error for a negative cap")
	}
	if *s.Sagittarius.Memory.MaxRunes != 0 {
		t.Fatalf("rejected value mutated Memory.MaxRunes to %v", *s.Sagittarius.Memory.MaxRunes)
	}

	if err := clearSettingValue(s, "sagittarius.memory.maxRunes"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if s.Sagittarius.Memory.MaxRunes != nil {
		t.Fatalf("clear should unset the key, got %v", *s.Sagittarius.Memory.MaxRunes)
	}
	if got := config.ResolveMemoryMaxRunes(s.Sagittarius); got != config.DefaultMemoryMaxRunes {
		t.Fatalf("after clear, resolver = %d, want the compiled-in default %d", got, config.DefaultMemoryMaxRunes)
	}
}

func TestApplySettingValueDefaultModeRejectsUnknownValue(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.defaultMode", "yolo"); err == nil {
		t.Fatal("expected an error for an unrecognized mode name")
	}
	if s.Sagittarius != nil && s.Sagittarius.DefaultMode != "" {
		t.Errorf("expected no mutation on a rejected value, got %q", s.Sagittarius.DefaultMode)
	}
}

func TestListSettingsEmptyDocumentDefaults(t *testing.T) {
	docs := loadEmptyDocs(t)
	entries := listSettings(docs, config.ScopeGlobal)

	boolRow := mustEntry(t, entries, "sagittarius.edit.enabled")
	if boolRow.Value != "" {
		t.Errorf("edit.enabled Value = %q, want empty", boolRow.Value)
	}
	if boolRow.DefaultValue != "true" {
		t.Errorf("edit.enabled DefaultValue = %q, want true", boolRow.DefaultValue)
	}
	if boolRow.DefinedHere || boolRow.Inherited {
		t.Errorf("edit.enabled DefinedHere=%v Inherited=%v, want both false", boolRow.DefinedHere, boolRow.Inherited)
	}

	intRow := mustEntry(t, entries, "sagittarius.maxToolRounds")
	wantRounds := strconv.Itoa(config.ResolveMaxToolRounds(nil, tools.MaxToolRounds))
	if intRow.DefaultValue != wantRounds {
		t.Errorf("maxToolRounds DefaultValue = %q, want %q", intRow.DefaultValue, wantRounds)
	}
	if intRow.Value != "" {
		t.Errorf("maxToolRounds Value = %q, want empty", intRow.Value)
	}

	enumRow := mustEntry(t, entries, "sagittarius.defaultMode")
	if enumRow.DefaultValue != "agent" {
		t.Errorf("defaultMode DefaultValue = %q, want agent", enumRow.DefaultValue)
	}
	if enumRow.Value != "" {
		t.Errorf("defaultMode Value = %q, want empty", enumRow.Value)
	}
}

func TestListSettingsInheritsFromGlobal(t *testing.T) {
	docs := loadEmptyDocs(t)
	if err := applySettingValue(docs.Global, "sagittarius.edit.enabled", "true"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	docs.ReloadMerged()

	entries := listSettings(docs, config.ScopeProject)
	e := mustEntry(t, entries, "sagittarius.edit.enabled")
	if e.DefinedHere {
		t.Fatal("project scope should not mark edit.enabled DefinedHere")
	}
	if !e.Inherited || e.MergedValue != "true" {
		t.Fatalf("Inherited=%v MergedValue=%q, want true/true", e.Inherited, e.MergedValue)
	}
	if e.Value != "" {
		t.Fatalf("Value = %q, want empty", e.Value)
	}
}

func TestListSettingsDefinedHereIsPerKey(t *testing.T) {
	docs := loadEmptyDocs(t)
	if err := docs.Global.SetUITheme("greyscale"); err != nil {
		t.Fatalf("SetUITheme: %v", err)
	}
	docs.ReloadMerged()

	entries := listSettings(docs, config.ScopeGlobal)
	theme := mustEntry(t, entries, "ui.theme")
	if !theme.DefinedHere || theme.Value != "greyscale" {
		t.Errorf("ui.theme DefinedHere=%v Value=%q, want true/greyscale", theme.DefinedHere, theme.Value)
	}
	hide := mustEntry(t, entries, "ui.hideBanner")
	if hide.DefinedHere {
		t.Error("ui.hideBanner DefinedHere=true when only ui.theme is set")
	}
	if hide.Value != "" {
		t.Errorf("ui.hideBanner Value = %q, want empty", hide.Value)
	}
}

func TestClearSettingValueUIEscapeAtOnPasteRemovesKey(t *testing.T) {
	s := &config.Settings{}
	if err := s.SetUIEscapeAtOnPaste(true); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := clearSettingValue(s, "ui.escapeAtOnPaste"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	raw, ok := s.Raw["ui"]
	if !ok {
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("decode ui: %v", err)
	}
	if _, exists := obj["escapeAtOnPaste"]; exists {
		t.Fatalf("escapeAtOnPaste still present: %s", raw)
	}
}

func TestApplySettingValueGoogleChat(t *testing.T) {
	s := &config.Settings{}

	if err := applySettingValue(s, "sagittarius.chat.googleChat.enabled", "true"); err != nil {
		t.Fatalf("apply enabled: %v", err)
	}
	if err := applySettingValue(s, "sagittarius.chat.googleChat.spaceId", "spaces/DM123"); err != nil {
		t.Fatalf("apply spaceId: %v", err)
	}
	if err := applySettingValue(s, "sagittarius.chat.googleChat.authorizedUsers", "users/1, rob@undeadindustries.com"); err != nil {
		t.Fatalf("apply authorizedUsers: %v", err)
	}
	if err := applySettingValue(s, "sagittarius.chat.googleChat.maxResultRunes", "1500"); err != nil {
		t.Fatalf("apply maxResultRunes: %v", err)
	}

	gc := s.Sagittarius.Chat.GoogleChat
	if gc == nil || gc.Enabled == nil || !*gc.Enabled {
		t.Errorf("enabled was not set")
	}
	if gc.SpaceID != "spaces/DM123" {
		t.Errorf("spaceId = %q, want spaces/DM123", gc.SpaceID)
	}
	if len(gc.AuthorizedUsers) != 2 || gc.AuthorizedUsers[0] != "users/1" || gc.AuthorizedUsers[1] != "rob@undeadindustries.com" {
		t.Errorf("authorizedUsers = %v", gc.AuthorizedUsers)
	}
	if gc.MaxResultRunes == nil || *gc.MaxResultRunes != 1500 {
		t.Errorf("maxResultRunes = %v", gc.MaxResultRunes)
	}

	// Test clear
	if err := clearSettingValue(s, "sagittarius.chat.googleChat.enabled"); err != nil {
		t.Fatalf("clear enabled: %v", err)
	}
	if gc.Enabled != nil {
		t.Errorf("enabled was not cleared")
	}
	if err := clearSettingValue(s, "sagittarius.chat.googleChat.spaceId"); err != nil {
		t.Fatalf("clear spaceId: %v", err)
	}
	if gc.SpaceID != "" {
		t.Errorf("spaceId was not cleared")
	}
}

func loadEmptyDocs(t *testing.T) *config.Documents {
	t.Helper()
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	docs, err := config.LoadDocuments(t.TempDir())
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	return docs
}

func mustEntry(t *testing.T, entries []settingsdialog.SettingEntry, key string) settingsdialog.SettingEntry {
	t.Helper()
	for _, e := range entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("missing setting %q", key)
	return settingsdialog.SettingEntry{}
}
