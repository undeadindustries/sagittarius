package providersdialog

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
)

func TestFreshSecretInputStartsBlank(t *testing.T) {
	ti := freshSecretInput()
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

func TestEnterSetKeyUsesBlankSecretField(t *testing.T) {
	deps := newFakeDeps()
	m := New(context.Background(), deps)
	m = m.enterSetKey()
	if m.input.Value() != "" {
		t.Fatalf("set-key value = %q, want empty", m.input.Value())
	}
}
