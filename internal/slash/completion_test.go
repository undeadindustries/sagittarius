package slash

import (
	"testing"
)

func labels(items []Suggestion) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		out = append(out, s.Label)
	}
	return out
}

func contains(items []Suggestion, label string) bool {
	for _, s := range items {
		if s.Label == label {
			return true
		}
	}
	return false
}

func TestCompleteTopLevelShowsAllCommands(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/", Deps{})

	for _, want := range []string{"help", "quit", "providers", "model", "models", "system-prompt", "modes", "mode", "mcp"} {
		if !contains(got.Items, want) {
			t.Errorf("top-level completion missing %q (got %v)", want, labels(got.Items))
		}
	}
	if got.ReplaceFrom != 1 {
		t.Errorf("ReplaceFrom = %d, want 1", got.ReplaceFrom)
	}
}

func TestCompletePrefixFiltersCommands(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/prov", Deps{})

	if len(got.Items) != 1 || got.Items[0].Label != "providers" {
		t.Fatalf("/prov completion = %v, want [providers]", labels(got.Items))
	}
	// /providers is now menu-first with no subcommands or arg completer, so it
	// is a terminal command — accepting it should submit, not append a space.
	if got.Items[0].AppendSpace {
		t.Error("providers suggestion should not append a space (menu-first, no subcommands)")
	}
	if got.ReplaceFrom != 1 {
		t.Errorf("ReplaceFrom = %d, want 1", got.ReplaceFrom)
	}
}

func TestCompleteModelAndModels(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/model", Deps{})

	// Both /model and /models should appear when typing "/model".
	if !contains(got.Items, "model") {
		t.Errorf("/model completion missing 'model' (got %v)", labels(got.Items))
	}
	if !contains(got.Items, "models") {
		t.Errorf("/model completion missing 'models' (got %v)", labels(got.Items))
	}
}

func TestCompleteSubcommandsStillWork(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/mcp ", Deps{})

	for _, want := range []string{"list", "reload"} {
		if !contains(got.Items, want) {
			t.Errorf("/mcp subcommand completion missing %q (got %v)", want, labels(got.Items))
		}
	}
	if got.ReplaceFrom != len("/mcp ") {
		t.Errorf("ReplaceFrom = %d, want %d", got.ReplaceFrom, len("/mcp "))
	}
}

func TestCompleteNonSlashEmpty(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("hello world", Deps{})
	if len(got.Items) != 0 {
		t.Errorf("non-slash input produced %v", labels(got.Items))
	}
}
