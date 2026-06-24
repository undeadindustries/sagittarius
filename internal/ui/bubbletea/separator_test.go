package bubbletea

import (
	"strings"
	"testing"
)

func TestInputSeparatorRendersRule(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	sep := stripANSI(m.renderInputSeparator())
	if !strings.Contains(sep, "─") {
		t.Fatalf("separator should contain a horizontal rule, got %q", sep)
	}
	if separatorRows != 1 {
		t.Fatalf("separatorRows = %d, want 1", separatorRows)
	}
}

func TestViewIncludesSeparatorAboveInput(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	out := stripANSI(m.View())
	if !strings.Contains(out, "───") {
		t.Fatalf("rendered view should include the input separator rule:\n%s", out)
	}
}
