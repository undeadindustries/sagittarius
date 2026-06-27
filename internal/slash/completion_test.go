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

	// Verify top-level commands are sorted alphabetically.
	for i := 1; i < len(got.Items); i++ {
		if got.Items[i-1].Label > got.Items[i].Label {
			t.Errorf("top-level completion not sorted: %q comes before %q", got.Items[i-1].Label, got.Items[i].Label)
		}
	}
}

func TestCompleteSubcommandsAreSorted(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()

	// Test /mode subcommands
	got := reg.Complete("/mode ", Deps{})
	if len(got.Items) < 6 {
		t.Fatalf("/mode completion items = %d, want at least 6", len(got.Items))
	}
	for i := 1; i < len(got.Items); i++ {
		if got.Items[i-1].Label > got.Items[i].Label {
			t.Errorf("/mode subcommand completion not sorted: %q comes before %q", got.Items[i-1].Label, got.Items[i].Label)
		}
	}

	// Verify /reasoning subcommands are NOT sorted
	got = reg.Complete("/reasoning ", Deps{})
	if len(got.Items) < 7 {
		t.Fatalf("/reasoning completion items = %d, want at least 7", len(got.Items))
	}
	// Verify order matches definition (minimal, low, medium, high)
	expectedOrder := []string{"show", "clear", "save", "minimal", "low", "medium", "high"}
	actual := labels(got.Items)
	for i, want := range expectedOrder {
		if i >= len(actual) || actual[i] != want {
			t.Errorf("/reasoning subcommands order wrong at index %d: want %q, got %q", i, want, actual[i])
			break
		}
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

func TestCompleteModesNoHeadlessSubcommands(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/modes ", Deps{})

	if len(got.Items) != 0 {
		t.Fatalf("/modes should not suggest headless subcommands (got %v)", labels(got.Items))
	}
	for _, hidden := range []string{"override", "clear"} {
		if contains(got.Items, hidden) {
			t.Errorf("/modes completion should not include hidden %q", hidden)
		}
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
