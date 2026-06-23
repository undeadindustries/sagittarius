package slash_test

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestStatsRegistered(t *testing.T) {
	t.Parallel()
	reg := slash.NewProcessor().Registry()

	paths := [][]string{
		{"stats"},
		{"stats", "session"},
		{"stats", "model"},
		{"stats", "tools"},
	}
	for _, path := range paths {
		cmd := reg.Lookup(path)
		if cmd == nil {
			t.Errorf("Lookup(%v) = nil, want command", path)
			continue
		}
		want := path[len(path)-1]
		if cmd.Name != want {
			t.Errorf("Lookup(%v).Name = %q, want %q", path, cmd.Name, want)
		}
	}

	if !strings.Contains(reg.RenderHelp(), "/stats") {
		t.Error("help missing /stats")
	}
}

func TestStatsPassesSection(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	p := slash.NewProcessor()

	tests := []struct {
		input string
		want  string
	}{
		{"/stats", "stats[session]"},
		{"/stats session", "stats[session]"},
		{"/stats model", "stats[model]"},
		{"/stats tools", "stats[tools]"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			res := p.Process(context.Background(), tt.input, deps)
			if res.Err != nil {
				t.Fatalf("unexpected error: %v", res.Err)
			}
			if got := strings.Join(res.Messages, "\n"); got != tt.want {
				t.Errorf("%s => %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStatsNilHooks(t *testing.T) {
	t.Parallel()
	deps, _, _ := testDeps(t, nil)
	deps.Hooks = nil
	p := slash.NewProcessor()

	res := p.Process(context.Background(), "/stats", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if got := strings.Join(res.Messages, "\n"); !strings.Contains(got, "not available") {
		t.Errorf("nil-hooks message = %q, want graceful message", got)
	}
}
