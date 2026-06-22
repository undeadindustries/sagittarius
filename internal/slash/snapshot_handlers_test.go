package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestDiffEmpty(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/diff", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if !strings.Contains(strings.Join(res.Messages, "\n"), "No file changes") {
		t.Fatalf("expected empty-stack message, got %v", res.Messages)
	}
}

func TestUndoInvalidArg(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/undo abc", deps)
	if res.Err == nil {
		t.Fatal("expected error for non-numeric undo arg")
	}
}

func TestDiffUndoRegistered(t *testing.T) {
	t.Parallel()
	help := slash.NewProcessor().Registry().RenderHelp()
	for _, want := range []string{"/diff", "/undo"} {
		if !strings.Contains(help, want) {
			t.Errorf("help missing %q", want)
		}
	}
}
