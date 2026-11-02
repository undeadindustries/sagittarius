package agent

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/customprompt"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// fakePromptLoader counts loads so tests can prove the file is read only on a
// name change or an explicit reload.
type fakePromptLoader struct {
	mu     sync.Mutex
	bodies map[string]string
	calls  map[string]int
}

func (f *fakePromptLoader) load(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[name]++
	body, ok := f.bodies[name]
	if !ok {
		return "", customprompt.ErrNotFound
	}
	return body, nil
}

func (f *fakePromptLoader) count(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func customSettings(name string) *config.Settings {
	return &config.Settings{Sagittarius: &config.SagittariusSettings{
		SystemPrompt: &config.SagittariusSystemPromptConfig{Custom: name},
	}}
}

func newCustomPromptRunner(t *testing.T, loader *fakePromptLoader, override string) *Runner {
	t.Helper()
	r, err := NewRunner(RunnerConfig{
		Generator:            &fakeGenerator{},
		Model:                "test-model",
		WorkDir:              t.TempDir(),
		SystemPromptOverride: override,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	r.loadCustomPrompt = loader.load
	return r
}

func systemText(r *Runner) string {
	r.modelMu.RLock()
	defer r.modelMu.RUnlock()
	return r.system
}

func TestRunnerCustomPromptCachesByName(t *testing.T) {
	t.Parallel()
	loader := &fakePromptLoader{
		bodies: map[string]string{"atlas": "You are Atlas.", "orion": "You are Orion."},
		calls:  map[string]int{},
	}
	r := newCustomPromptRunner(t, loader, "")

	r.SetSettings(customSettings("atlas"))
	if !strings.HasPrefix(systemText(r), "You are Atlas.") {
		t.Fatalf("system prompt does not start with the custom body:\n%.200s", systemText(r))
	}
	if got := r.ActiveCustomPrompt(); got != "atlas" {
		t.Fatalf("ActiveCustomPrompt = %q, want atlas", got)
	}

	r.rebuildSystem()
	r.rebuildSystem()
	if n := loader.count("atlas"); n != 1 {
		t.Fatalf("atlas loaded %d times across rebuilds, want 1", n)
	}

	loader.mu.Lock()
	loader.bodies["atlas"] = "You are Atlas v2."
	loader.mu.Unlock()
	r.rebuildSystem()
	if strings.Contains(systemText(r), "Atlas v2") {
		t.Fatal("a plain rebuild must not re-read the file")
	}
	if err := r.ReloadSystemInstruction(); err != nil {
		t.Fatalf("ReloadSystemInstruction: %v", err)
	}
	if !strings.HasPrefix(systemText(r), "You are Atlas v2.") {
		t.Fatal("reload should pick up the new text")
	}

	r.SetSettings(customSettings("orion"))
	if !strings.HasPrefix(systemText(r), "You are Orion.") {
		t.Fatal("name change should load the new prompt")
	}

	r.SetSettings(&config.Settings{})
	if strings.Contains(systemText(r), "You are Orion.") || r.ActiveCustomPrompt() != "" {
		t.Fatal("clearing the selection should restore the built-in prompt")
	}
	if !strings.Contains(systemText(r), "You are Sagittarius,") {
		t.Fatal("built-in identity missing after clearing the custom selection")
	}
}

func TestRunnerCustomPromptMissingFallsBackOnce(t *testing.T) {
	t.Parallel()
	loader := &fakePromptLoader{bodies: map[string]string{}, calls: map[string]int{}}
	r := newCustomPromptRunner(t, loader, "")

	r.SetSettings(customSettings("ghost"))
	if !strings.Contains(systemText(r), "You are Sagittarius,") {
		t.Fatal("missing custom prompt should fall back to the built-in prompt")
	}
	if r.ActiveCustomPrompt() != "" {
		t.Fatal("ActiveCustomPrompt should be empty after a failed load")
	}
	r.rebuildSystem()
	if n := loader.count("ghost"); n != 1 {
		t.Fatalf("failed load retried %d times, want 1", n)
	}

	events, err := r.RunTurn(testContext(t), "hi")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	notices := 0
	for _, ev := range collectEvents(t, events) {
		if ev.Type == ui.StreamInfo && strings.Contains(ev.Text, `"ghost"`) {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("fallback notices = %d, want 1", notices)
	}
	if n := r.takeCustomPromptNotice(); n != "" {
		t.Fatalf("notice should be consumed, got %q", n)
	}
}

func TestRunnerSystemPromptOverrideBeatsCustom(t *testing.T) {
	t.Parallel()
	loader := &fakePromptLoader{bodies: map[string]string{"atlas": "You are Atlas."}, calls: map[string]int{}}
	r := newCustomPromptRunner(t, loader, "EVALUATOR ONLY")

	r.SetSettings(customSettings("atlas"))
	if got := systemText(r); got != "EVALUATOR ONLY" {
		t.Fatalf("override should replace everything, got %.200q", got)
	}
	if loader.count("atlas") != 0 {
		t.Fatal("override path must not load the custom prompt")
	}
	if r.ActiveCustomPrompt() != "" {
		t.Fatal("ActiveCustomPrompt should be empty under an override")
	}
}

func TestLoadStoredCustomPromptUsesHome(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	store, err := customprompt.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save("atlas", "Stored body."); err != nil {
		t.Fatal(err)
	}
	got, err := loadStoredCustomPrompt("atlas")
	if err != nil || got != "Stored body." {
		t.Fatalf("loadStoredCustomPrompt = %q, %v", got, err)
	}
	if _, err := loadStoredCustomPrompt("missing"); !errors.Is(err, customprompt.ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
}
