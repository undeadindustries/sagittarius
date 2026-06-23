package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestCopyRegistered(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()
	if !strings.Contains(help, "/copy") {
		t.Fatalf("help missing /copy\n%s", help)
	}
}

func TestCopySetsClipboard(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Clipboard != "assistant reply" {
		t.Fatalf("Clipboard = %q, want %q", res.Clipboard, "assistant reply")
	}
}

func TestCopyNoHistory(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = ""
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Clipboard != "" {
		t.Fatalf("Clipboard = %q, want empty", res.Clipboard)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "No assistant response") {
		t.Fatalf("missing no-response message: %q", joined)
	}
}
