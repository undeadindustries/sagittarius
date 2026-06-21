package slash

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
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

	for _, want := range []string{"help", "quit", "providers", "model", "mode", "mcp"} {
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
	if !got.Items[0].AppendSpace {
		t.Error("providers suggestion should append a space (has subcommands)")
	}
	// Token starts right after the leading slash.
	if got.ReplaceFrom != 1 {
		t.Errorf("ReplaceFrom = %d, want 1", got.ReplaceFrom)
	}
}

func TestCompleteSubcommands(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/providers ", Deps{})

	for _, want := range []string{"list", "use", "show", "set", "add", "remove"} {
		if !contains(got.Items, want) {
			t.Errorf("subcommand completion missing %q (got %v)", want, labels(got.Items))
		}
	}
	if got.ReplaceFrom != len("/providers ") {
		t.Errorf("ReplaceFrom = %d, want %d", got.ReplaceFrom, len("/providers "))
	}
}

func TestCompleteProviderArgListsProviders(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	deps := Deps{Settings: &config.Settings{Providers: &config.ProvidersSettings{
		Custom: map[string]config.CustomProviderDefinition{"my-vllm": {BaseURL: "http://x/v1"}},
	}}}

	got := reg.Complete("/providers use ", deps)
	for _, want := range []string{"gemini", "openai", "openai-responses", "my-vllm"} {
		if !contains(got.Items, want) {
			t.Errorf("provider-arg completion missing %q (got %v)", want, labels(got.Items))
		}
	}
	if got.ReplaceFrom != len("/providers use ") {
		t.Errorf("ReplaceFrom = %d, want %d", got.ReplaceFrom, len("/providers use "))
	}
}

func TestCompleteProviderArgPrefixFilter(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	got := reg.Complete("/providers use ope", Deps{})

	for _, s := range got.Items {
		if !strings.HasPrefix(s.Label, "ope") {
			t.Errorf("prefix filter leaked %q", s.Label)
		}
	}
	if !contains(got.Items, "openai") || !contains(got.Items, "openai-responses") {
		t.Errorf("expected openai variants, got %v", labels(got.Items))
	}
	if got.ReplaceFrom != len("/providers use ") {
		t.Errorf("ReplaceFrom = %d, want %d (token start)", got.ReplaceFrom, len("/providers use "))
	}
}

func TestCompleteRemoveOnlyCustom(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	deps := Deps{Settings: &config.Settings{Providers: &config.ProvidersSettings{
		Custom: map[string]config.CustomProviderDefinition{"my-vllm": {BaseURL: "http://x/v1"}},
	}}}

	got := reg.Complete("/providers remove ", deps)
	if !contains(got.Items, "my-vllm") {
		t.Errorf("remove completion missing custom provider (got %v)", labels(got.Items))
	}
	if contains(got.Items, "openai") {
		t.Error("remove completion should not list built-in providers")
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
