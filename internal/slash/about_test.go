package slash_test

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestAboutCommand(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/about", deps)
	if res.Err != nil {
		t.Fatalf("/about error: %v", res.Err)
	}
	combined := strings.Join(res.Messages, "\n")
	for _, want := range []string{"Sagittarius CLI", runtime.Version()} {
		if !strings.Contains(combined, want) {
			t.Errorf("/about output missing %q\n%s", want, combined)
		}
	}
}

func TestAboutRegistered(t *testing.T) {
	t.Parallel()
	help := slash.NewProcessor().Registry().RenderHelp()
	if !strings.Contains(help, "/about") {
		t.Errorf("help missing /about\n%s", help)
	}
}
