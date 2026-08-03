package slash_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestChatRegistered(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()
	if !strings.Contains(help, "/chat") {
		t.Fatalf("help missing /chat\n%s", help)
	}
}

func TestChatDebug(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat debug", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "sagittarius-request-test.json") {
		t.Fatalf("debug output missing request file path: %q", joined)
	}
}

func TestChatResumeScrollback(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat resume mytag", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if len(res.Scrollback) != 2 {
		t.Fatalf("expected 2 restored scrollback entries, got %d", len(res.Scrollback))
	}
	if res.Scrollback[0].Role != slash.ScrollUser || res.Scrollback[1].Role != slash.ScrollAssistant {
		t.Fatalf("unexpected scrollback roles: %+v", res.Scrollback)
	}
}

func TestChatShareRejectsBadExtension(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat share notes.txt", deps)
	if res.Err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	if !strings.Contains(res.Err.Error(), "only .md and .json") {
		t.Fatalf("unexpected error: %v", res.Err)
	}
}

func TestChatList(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat list", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	joined := strings.Join(res.Messages, "\n")
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(joined, want) {
			t.Errorf("list output missing %q\n%s", want, joined)
		}
	}
}

func TestChatSave(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat save mytag", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "mytag") {
		t.Fatalf("save output missing tag: %q", joined)
	}
}

func TestChatShareMarkdown(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	dir := t.TempDir()
	hooks.workDir = dir
	out := filepath.Join(dir, "conversation.md")

	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/chat share "+out, deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read share file: %v", err)
	}
	if !strings.Contains(string(data), "# Conversation") {
		t.Fatalf("share file missing header:\n%s", data)
	}
}

func TestChatRename(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat rename Fix LSP pool race", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if hooks.renamedTitle != "Fix LSP pool race" {
		t.Fatalf("renamed title = %q, want %q", hooks.renamedTitle, "Fix LSP pool race")
	}
}

func TestChatRenameSanitizes(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	// Newlines, carriage returns and control chars are stripped; title survives.
	raw := "fix the\nLSP pool\r\nrace\tnow\x01"
	res := p.Process(context.Background(), "/chat rename "+raw, deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	got := hooks.renamedTitle
	if strings.ContainsAny(got, "\n\r\x01") {
		t.Fatalf("control characters survived sanitize: %q", got)
	}
	if !strings.Contains(got, "LSP pool") {
		t.Fatalf("content lost during sanitize: %q", got)
	}
}

func TestChatRenameEmptyRejected(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat rename   ", deps)
	if res.Err == nil {
		t.Fatal("expected error for empty title")
	}
	if !strings.Contains(res.Err.Error(), "cannot be empty") {
		t.Fatalf("unexpected error: %v", res.Err)
	}
}

func TestChatRenameTruncatesLongTitle(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	p := slash.NewProcessor()

	long := strings.Repeat("word ", 40) // 200 runes
	res := p.Process(context.Background(), "/chat rename "+long, deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if n := len([]rune(hooks.renamedTitle)); n > 80 {
		t.Fatalf("title not capped at 80 runes: %d", n)
	}
}

func TestChatFork(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/chat fork", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "forked-session-id") {
		t.Fatalf("fork output missing new session id: %q", joined)
	}
}
