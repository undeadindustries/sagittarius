package slash_test

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestMouseRegistered(t *testing.T) {
	t.Parallel()
	reg := slash.NewRegistry()
	paths := [][]string{
		{"mouse"},
		{"mouse", "on"},
		{"mouse", "off"},
		{"mouse", "toggle"},
		{"mouse", "show"},
	}
	for _, p := range paths {
		if reg.Lookup(p) == nil {
			t.Errorf("Lookup(%v) = nil, want command", p)
		}
	}
}

func TestMouseSetsMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input string
		want  string
	}{
		{"/mouse on", "on"},
		{"/mouse off", "off"},
		{"/mouse toggle", "toggle"},
		{"/mouse", "toggle"},
	}
	p := slash.NewProcessor()
	for _, tc := range cases {
		res := p.Process(context.Background(), tc.input, slash.Deps{})
		if !res.Handled {
			t.Errorf("%q: not handled", tc.input)
		}
		if res.MouseMode != tc.want {
			t.Errorf("%q: MouseMode = %q, want %q", tc.input, res.MouseMode, tc.want)
		}
	}
}

func TestMouseShowNoLiveSwitch(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/mouse show", slash.Deps{})
	if res.MouseMode != "" {
		t.Errorf("show MouseMode = %q, want empty", res.MouseMode)
	}
	if len(res.Messages) == 0 {
		t.Error("show should return an informational message")
	}
}
