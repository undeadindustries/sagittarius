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

func TestCopyCodeDefaultsToLastBlock(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "```\nfirst\n```\n```\nsecond\n```"
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy code", deps)
	if res.Clipboard != "second" {
		t.Fatalf("Clipboard = %q, want last block 'second'", res.Clipboard)
	}
}

func TestCopyCodeSpecificIndexAndAll(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "```\nblock1\n```\nprose\n```\nblock2\n```\nprose\n```\nblock3\n```"
	p := slash.NewProcessor()

	tests := []struct {
		input       string
		wantClip    string
		wantMsgPart string
	}{
		{input: "/copy code 1", wantClip: "block1"},
		{input: "/copy code 2", wantClip: "block2"},
		{input: "/copy code 3", wantClip: "block3"},
		{input: "/copy code all", wantClip: "block1\n\nblock2\n\nblock3"},
		{input: "/copy code 0", wantMsgPart: "Valid range is 1 to 3, or 'all'"},
		{input: "/copy code 4", wantMsgPart: "Valid range is 1 to 3, or 'all'"},
		{input: "/copy code foo", wantMsgPart: "Valid range is 1 to 3, or 'all'"},
	}

	for _, tc := range tests {
		res := p.Process(context.Background(), tc.input, deps)
		if tc.wantClip != "" && res.Clipboard != tc.wantClip {
			t.Errorf("%s: Clipboard = %q, want %q", tc.input, res.Clipboard, tc.wantClip)
		}
		if tc.wantMsgPart != "" {
			joined := strings.Join(res.Messages, "\n")
			if !strings.Contains(joined, tc.wantMsgPart) {
				t.Errorf("%s: message %q does not contain %q", tc.input, joined, tc.wantMsgPart)
			}
		}
	}
}

func TestCopyCodeUnclosedFence(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	hooks.lastAssistant = "```\nstarted block\nstill going"
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/copy code", deps)
	want := "started block\nstill going"
	if res.Clipboard != want {
		t.Fatalf("Clipboard = %q, want %q", res.Clipboard, want)
	}
}
