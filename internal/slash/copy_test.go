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
	if !strings.Contains(help, "/copy code") {
		t.Fatalf("help missing /copy code\n%s", help)
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

func TestCopyKeepsMarkdownFences(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	raw := "See:\n```\necho hi\n```"
	hooks.lastAssistant = raw
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy", deps)
	if res.Clipboard != raw {
		t.Fatalf("Clipboard = %q, want full reply %q", res.Clipboard, raw)
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

func TestCopyCodeExtractsFences(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "Run:\n```bash\neval \"$(starship init bash)\"\n```\nThen reboot."
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy code", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	want := "eval \"$(starship init bash)\""
	if res.Clipboard != want {
		t.Fatalf("Clipboard = %q, want %q", res.Clipboard, want)
	}
}

func TestCopyCodeNoFences(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "No commands here, just advice."
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy code", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.Clipboard != "" {
		t.Fatalf("Clipboard = %q, want empty", res.Clipboard)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "No fenced code") {
		t.Fatalf("missing no-fences message: %q", joined)
	}
}

func TestCopyCodeJoinsMultipleBlocks(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "```\nfirst\n```\n```\nsecond\n```"
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy code", deps)
	if res.Clipboard != "first\n\nsecond" {
		t.Fatalf("Clipboard = %q, want joined blocks", res.Clipboard)
	}
}
