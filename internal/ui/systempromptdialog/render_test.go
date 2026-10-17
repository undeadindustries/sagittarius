package systempromptdialog

import (
	"context"
	"strings"
	"testing"
)

type stubDeps struct{}

func (stubDeps) CurrentPresetID() string { return "" }
func (stubDeps) ApplyPreset(context.Context, string) (string, error) {
	return "", nil
}

func TestViewFitsShortTerminal(t *testing.T) {
	const height = 10
	base := New(context.Background(), stubDeps{}).SetSize(80, height)
	if len(base.options) < 3 {
		t.Skipf("only %d presets; nothing to window", len(base.options))
	}
	for _, cursor := range []int{0, len(base.options) - 1} {
		m := base
		m.cursor = cursor
		view := m.View()
		if got := strings.Count(view, "\n") + 1; got > height {
			t.Errorf("cursor %d: view is %d lines, want <= %d:\n%s", cursor, got, height, view)
		}
		for _, want := range []string{"System Prompt", "Esc close", "> " + m.options[cursor].label} {
			if !strings.Contains(view, want) {
				t.Errorf("cursor %d: view lost %q:\n%s", cursor, want, view)
			}
		}
	}
}
