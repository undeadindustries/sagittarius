package agent

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestWrapIndex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		idx, step, n, want int
	}{
		{0, +1, 3, 1},
		{2, +1, 3, 0}, // forward wrap
		{0, -1, 3, 2}, // reverse wrap
		{1, -1, 3, 0},
		{0, +1, 1, 0}, // single element
		{0, +1, 0, 0}, // empty guard
	}
	for _, tc := range cases {
		if got := wrapIndex(tc.idx, tc.step, tc.n); got != tc.want {
			t.Errorf("wrapIndex(%d,%d,%d) = %d, want %d", tc.idx, tc.step, tc.n, got, tc.want)
		}
	}
}

func TestCycleThemeToggles(t *testing.T) {
	t.Parallel()
	s := &config.Settings{}
	// Loader nil: CycleTheme skips persistence but still toggles in memory.
	app := &App{deps: slash.Deps{Settings: s}}

	first, err := app.CycleTheme()
	if err != nil {
		t.Fatalf("CycleTheme: %v", err)
	}
	if first != "greyscale" {
		t.Fatalf("first cycle = %q, want greyscale", first)
	}
	if got := s.UI().Theme; got != "greyscale" {
		t.Fatalf("settings theme = %q, want greyscale", got)
	}

	second, err := app.CycleTheme()
	if err != nil {
		t.Fatalf("CycleTheme: %v", err)
	}
	if second != "default" {
		t.Fatalf("second cycle = %q, want default", second)
	}
}

func TestCycleThemeNoSettings(t *testing.T) {
	t.Parallel()
	app := &App{}
	if _, err := app.CycleTheme(); err == nil {
		t.Fatal("CycleTheme without settings should error")
	}
}

func TestSetModeByNameRejectsUnknown(t *testing.T) {
	t.Parallel()
	// Non-nil runner passes the availability guard; ParseMode then rejects the
	// bad name before any slash processing runs.
	app := &App{runner: &Runner{}}
	if _, err := app.SetModeByName(context.Background(), "bogus"); err == nil {
		t.Fatal("SetModeByName with an unknown mode should error")
	}
}

func TestCycleModelNilRunner(t *testing.T) {
	t.Parallel()
	app := &App{}
	if _, err := app.CycleModel(context.Background()); err == nil {
		t.Fatal("CycleModel without a runner should error")
	}
	if _, err := app.CycleModelReverse(context.Background()); err == nil {
		t.Fatal("CycleModelReverse without a runner should error")
	}
}
