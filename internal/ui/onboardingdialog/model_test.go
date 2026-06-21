package onboardingdialog

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fakeDeps struct {
	preparedID string
	models     []string
	complete   struct {
		providerID string
		model      string
	}
}

func (f *fakeDeps) PrepareGemini(_ context.Context, _ string) (string, error) {
	f.preparedID = "gemini-apikey"
	return f.preparedID, nil
}

func (f *fakeDeps) PrepareOpenRouter(_ context.Context, _ string) (string, error) {
	f.preparedID = OpenRouterProviderID
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
	m.choice = choiceGemini
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
