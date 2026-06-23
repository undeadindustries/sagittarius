package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestCompressRegistered(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()
	if !strings.Contains(help, "/compress") {
		t.Fatalf("help missing /compress\n%s", help)
	}
}

func TestCompressInvokesHook(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/compress", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "Compressed context") {
		t.Fatalf("compress output missing summary: %q", joined)
	}
}
