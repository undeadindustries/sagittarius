package providersdialog

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

func TestFreshSecretInputStartsBlank(t *testing.T) {
	ti := freshSecretInput(theme.Default())
	if ti.Value() != "" {
		t.Fatalf("secret input value = %q, want empty", ti.Value())
	}
	if ti.Placeholder != "" {
		t.Fatalf("secret input placeholder = %q, want empty", ti.Placeholder)
	}
	if ti.EchoMode != textinput.EchoPassword {
		t.Fatalf("echo mode = %v, want EchoPassword", ti.EchoMode)
	}
	view := ti.View()
	if strings.Contains(view, "> p") || strings.Contains(view, "> paste") {
		t.Fatalf("secret input view leaked placeholder: %q", view)
	}
}

func TestGreyscalePlaceholderHasNoColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	lipgloss.SetColorProfile(termenv.TrueColor)
	ti := freshInput(theme.Greyscale(), "model name")
	ti.Width = 40
	view := ti.View()
	if strings.Contains(view, "38;") || strings.Contains(view, "48;") {
		t.Fatalf("greyscale placeholder has color: %q", view)
	}
	if !strings.Contains(view, "model name") {
		t.Fatalf("placeholder missing: %q", view)
	}
}

func TestEnterSetKeyUsesBlankSecretField(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)
	m = m.enterSetKey()
	if m.input.Value() != "" {
		t.Fatalf("set-key value = %q, want empty", m.input.Value())
	}
}
