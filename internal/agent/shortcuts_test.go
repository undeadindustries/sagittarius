package agent

import (
	"context"
	"slices"
	"strings"
	"sync"
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

func TestReasoningCycle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		efforts   []string
		mandatory bool
		known     bool
		want      []string
	}{
		{"unknown model gets the generic levels", nil, false, false, []string{"", "none", "low", "medium", "high"}},
		{"optional reasoning adds none", []string{"low", "medium", "high"}, false, true, []string{"", "none", "low", "medium", "high"}},
		{"none already listed is not doubled", []string{"none", "low", "high"}, false, true, []string{"", "none", "low", "high"}},
		{"off counts as none", []string{"off", "high"}, false, true, []string{"", "off", "high"}},
		{"mandatory never offers none", []string{"none", "low", "high", "xhigh"}, true, true, []string{"", "low", "high", "xhigh"}},
		{"not reasoning-capable has nothing to cycle", nil, false, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := reasoningCycle(tc.efforts, tc.mandatory, tc.known); !slices.Equal(got, tc.want) {
				t.Errorf("reasoningCycle = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReasoningCycleDoesNotAliasGeneric(t *testing.T) {
	t.Parallel()
	got := reasoningCycle(nil, false, false)
	got[1] = "mutated"
	if genericReasoningCycle[1] != reasoningOff {
		t.Fatal("reasoningCycle returned the shared generic slice")
	}
}

func TestNextInCycle(t *testing.T) {
	t.Parallel()
	cycle := []string{"", "none", "low", "high"}
	cases := []struct{ current, want string }{
		{"", "none"},
		{"none", "low"},
		{"high", ""},
		{"xhigh", "none"}, // set via /reasoning outside the cycle: restart at the first level
	}
	for _, tc := range cases {
		if got := nextInCycle(cycle, tc.current); got != tc.want {
			t.Errorf("nextInCycle(%q) = %q, want %q", tc.current, got, tc.want)
		}
	}
}

// TestAppCycleReasoningEffortLapsAndFeedsRequest drives Alt+R through a full
// lap on a Gemini model: every step lands in the runner override, the next
// request, and the footer, and the lap ends back on the model default.
func TestAppCycleReasoningEffortLapsAndFeedsRequest(t *testing.T) {
	t.Parallel()
	const model = "gemini-2.5-flash"
	r := newThoughtsTestRunner(t, model, nil)
	app := &App{runner: r, deps: slash.Deps{Settings: r.settings}}

	seen := map[string]bool{}
	for step := 0; step < 10; step++ {
		level, desc, err := app.CycleReasoningEffort()
		if err != nil {
			t.Fatalf("CycleReasoningEffort: %v", err)
		}
		if !strings.Contains(desc, model) {
			t.Errorf("description %q does not name the model", desc)
		}
		if got := r.ReasoningOverride(); got != level {
			t.Fatalf("override = %q after step %d, want %q", got, step, level)
		}
		if got := app.ComposerStatus().ReasoningEffort; got != level {
			t.Errorf("footer effort = %q, want %q", got, level)
		}
		if level == "" {
			if !strings.Contains(desc, "model default") {
				t.Errorf("clearing description = %q, want it to name the model default", desc)
			}
			if len(seen) == 0 {
				t.Fatal("the first press cleared the override instead of picking a level")
			}
			return
		}
		if req := r.buildGenerateRequest(); req.Reasoning == nil || req.Reasoning.Effort != level {
			t.Fatalf("request reasoning = %+v, want effort %q", req.Reasoning, level)
		}
		seen[level] = true
	}
	t.Fatalf("cycle never wrapped back to the default; saw %v", seen)
}

func TestAppCycleReasoningEffortWithoutRunner(t *testing.T) {
	t.Parallel()
	if _, _, err := (&App{}).CycleReasoningEffort(); err == nil {
		t.Fatal("want an error without a runner")
	}
	if _, _, err := (&App{}).ToggleYolo(); err == nil {
		t.Fatal("want an error without a runner")
	}
}

func newYoloTestRunner(t *testing.T, mode ApprovalMode, grants ...string) *Runner {
	t.Helper()
	r, err := NewRunner(RunnerConfig{
		Generator:            &fakeGenerator{},
		Model:                "test-model",
		WorkDir:              t.TempDir(),
		ApprovalMode:         mode,
		InitialSessionGrants: grants,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return r
}

func TestRunnerToggleYoloRestoresInitialMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		start   ApprovalMode
		offMode ApprovalMode
	}{
		{ApprovalDefault, ApprovalDefault},
		{ApprovalAutoEdit, ApprovalAutoEdit},
		{ApprovalYolo, ApprovalDefault}, // started in yolo: off has to mean asking again
	}
	for _, tc := range cases {
		t.Run(string(tc.start), func(t *testing.T) {
			t.Parallel()
			r := newYoloTestRunner(t, tc.start)
			first, firstYolo := r.ToggleYolo()
			second, secondYolo := r.ToggleYolo()
			if tc.start == ApprovalYolo {
				if first != tc.offMode || firstYolo {
					t.Fatalf("first toggle from yolo = (%s, %v), want (%s, false)", first, firstYolo, tc.offMode)
				}
				if second != ApprovalYolo || !secondYolo {
					t.Fatalf("second toggle = (%s, %v), want yolo", second, secondYolo)
				}
				return
			}
			if first != ApprovalYolo || !firstYolo {
				t.Fatalf("first toggle = (%s, %v), want yolo", first, firstYolo)
			}
			if second != tc.offMode || secondYolo {
				t.Fatalf("second toggle = (%s, %v), want (%s, false)", second, secondYolo, tc.offMode)
			}
			if got := r.ApprovalMode(); got != tc.offMode {
				t.Fatalf("ApprovalMode = %s, want %s", got, tc.offMode)
			}
		})
	}
}

func TestRunnerToggleYoloRebuildsSchedulerKeepingGrants(t *testing.T) {
	t.Parallel()
	r := newYoloTestRunner(t, ApprovalDefault, "run_shell_command")
	r.regMu.RLock()
	before := r.scheduler
	r.regMu.RUnlock()

	r.ToggleYolo()

	r.regMu.RLock()
	after := r.scheduler
	r.regMu.RUnlock()
	if after == before {
		t.Fatal("ToggleYolo did not rebuild the scheduler, so the new policy never applies")
	}
	if grants := strings.Join(after.SessionGrants(), ","); !strings.Contains(grants, "run_shell_command") {
		t.Fatalf("session grants lost across the rebuild: %q", grants)
	}
}

// TestRunnerToggleYoloConcurrentReads runs under -race: the footer polls
// ApprovalMode every frame while Alt+Y swaps it.
func TestRunnerToggleYoloConcurrentReads(t *testing.T) {
	t.Parallel()
	r := newYoloTestRunner(t, ApprovalDefault)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				_ = r.ApprovalMode()
			}
		}()
	}
	for range 10 {
		r.ToggleYolo()
	}
	wg.Wait()
	if got := r.ApprovalMode(); got != ApprovalDefault {
		t.Fatalf("after an even number of toggles ApprovalMode = %s, want default", got)
	}
}
