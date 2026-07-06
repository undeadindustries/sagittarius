package onboardingdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/config"
)

type fakeDeps struct {
	preparedID    string
	presetID      string
	models        []string
	complete      struct {
		providerID string
		model      string
	}
}

func (f *fakeDeps) PrepareGemini(_ context.Context, _ string) (string, error) {
	f.preparedID = "gemini-apikey"
	return f.preparedID, nil
}

func (f *fakeDeps) PreparePreset(_ context.Context, presetID, _ string) (string, error) {
	f.presetID = presetID
	f.preparedID = presetID
	return f.preparedID, nil
}

func (f *fakeDeps) PrepareCustom(_ context.Context, _, _ string) (string, error) {
	f.preparedID = "local-vllm"
	return f.preparedID, nil
}

func (f *fakeDeps) DiscoverModels(_ context.Context, _ string) ([]string, error) {
	return f.models, nil
}

func (f *fakeDeps) CompleteSetup(_ context.Context, providerID, model string) error {
	f.complete.providerID = providerID
	f.complete.model = model
	return nil
}

func TestChooseGeminiOpensAPIKeyScreen(t *testing.T) {
	t.Parallel()
	m := New(context.Background(), &fakeDeps{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if next.screen != screenAPIKey {
		t.Fatalf("screen = %v, want screenAPIKey", next.screen)
	}
}

func TestSubmitAPIKeyDiscoversModels(t *testing.T) {
	t.Parallel()
	deps := &fakeDeps{models: []string{"gemini-2.5-flash", "gemini-2.5-pro"}}
	m := New(context.Background(), deps)
	m.screen = screenAPIKey
	m.choice = onboardChoice{kind: choiceKindGemini}
	m.input.SetValue("secret-key")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected discover command")
	}
	if next.screen != screenModels || !next.loading {
		t.Fatalf("screen=%v loading=%v, want models+loading", next.screen, next.loading)
	}

	msg := cmd()
	loaded, ok := msg.(modelsLoadedMsg)
	if !ok {
		t.Fatalf("cmd returned %T, want modelsLoadedMsg", msg)
	}
	done := next.handleModelsLoaded(loaded)
	if done.loading || len(done.models) != 2 {
		t.Fatalf("after load: loading=%v models=%v", done.loading, done.models)
	}
}

// TestChooseListIncludesPresets verifies the connect-method list offers Gemini,
// every preset, and the custom flow.
func TestChooseListIncludesPresets(t *testing.T) {
	t.Parallel()
	m := New(context.Background(), &fakeDeps{})
	if len(m.choices) != len(config.ProviderPresets)+2 {
		t.Fatalf("choices = %d, want %d (gemini + presets + custom)", len(m.choices), len(config.ProviderPresets)+2)
	}
	if m.choices[0].kind != choiceKindGemini {
		t.Fatal("first choice should be Gemini")
	}
	if last := m.choices[len(m.choices)-1]; last.kind != choiceKindCustom {
		t.Fatal("last choice should be Custom")
	}
}

// TestSelectPresetPreparesByID verifies choosing a preset routes the API key to
// PreparePreset with the preset id and begins discovery.
func TestSelectPresetPreparesByID(t *testing.T) {
	t.Parallel()
	deps := &fakeDeps{models: []string{"m1"}}
	m := New(context.Background(), deps)

	// Locate the openrouter preset row.
	idx := -1
	for i, c := range m.choices {
		if c.kind == choiceKindPreset && c.presetID == "openrouter" {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("openrouter preset missing from choices")
	}
	m.cursor = idx
	m, _ = m.activate() // choose → API key screen
	if m.screen != screenAPIKey {
		t.Fatalf("screen = %v, want screenAPIKey", m.screen)
	}
	m.input.SetValue("sk-test")
	next, cmd := m.activate()
	if cmd == nil {
		t.Fatal("expected discover command after preset key")
	}
	if deps.presetID != "openrouter" {
		t.Fatalf("PreparePreset id = %q, want openrouter", deps.presetID)
	}
	if next.targetID != "openrouter" {
		t.Fatalf("targetID = %q, want openrouter", next.targetID)
	}
}

func TestSelectModelCompletesSetup(t *testing.T) {
	t.Parallel()
	deps := &fakeDeps{models: []string{"gpt-4o-mini"}}
	m := New(context.Background(), deps)
	m.screen = screenModels
	m.targetID = OpenRouterProviderID
	m.models = deps.models
	m.loading = false

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !next.Done() {
		t.Fatal("expected dialog done after model selection")
	}
	if deps.complete.providerID != OpenRouterProviderID || deps.complete.model != "gpt-4o-mini" {
		t.Fatalf("CompleteSetup = %+v", deps.complete)
	}
}
