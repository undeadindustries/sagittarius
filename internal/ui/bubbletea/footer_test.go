package bubbletea

import (
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// footerMetricsApp is a minimal app that exposes session metrics for footer tests.
type footerMetricsApp struct {
	quitApp
	stats ui.SessionStats
}

func (a footerMetricsApp) SessionMetrics() ui.SessionStats { return a.stats }

func TestFooterPerTurnTokensShown(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  500,
			LastOutputTokens: 120,
			InputTokens:      1200,
			OutputTokens:     340,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	right := stripANSI(status.Right)

	if !strings.Contains(right, "↑500") {
		t.Errorf("footer Right missing per-turn in-tokens: %q", right)
	}
	if !strings.Contains(right, "↓120") {
		t.Errorf("footer Right missing per-turn out-tokens: %q", right)
	}
}

func TestFooterSessionTotalInDetail(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  500,
			LastOutputTokens: 120,
			InputTokens:      1200,
			OutputTokens:     340,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	detail := stripANSI(status.Detail)

	if !strings.Contains(detail, "Σ") {
		t.Errorf("footer Detail missing session Σ: %q", detail)
	}
	if !strings.Contains(detail, "1.2k") {
		t.Errorf("footer Detail missing session in-tokens (1.2k): %q", detail)
	}
}

func TestFooterOpenRouterCostShown(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			Provider:         "openrouter",
			LastInputTokens:  200,
			LastOutputTokens: 80,
			LastCostUSD:      0.0021,
			LastCostKnown:    true,
			InputTokens:      400,
			OutputTokens:     160,
			SessionCostUSD:   0.0042,
			SessionCostKnown: true,
			ModelUsage: []ui.ModelUsageStat{
				{Provider: "openrouter", Model: "mistral/7b", Mode: "agent", CostKnown: true, CostUSD: 0.0042},
			},
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	right := stripANSI(status.Right)
	detail := stripANSI(status.Detail)

	if !strings.Contains(right, "$0.0021") {
		t.Errorf("footer Right missing last-turn cost: %q", right)
	}
	if !strings.Contains(detail, "$0.0042") {
		t.Errorf("footer Detail missing session cost: %q", detail)
	}
}

func TestFooterSessionCostHiddenOnNonCostProvider(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			Provider:         "gemini",
			LastInputTokens:  555100,
			LastOutputTokens: 257,
			LastCostKnown:    false,
			InputTokens:      149962000,
			OutputTokens:     65900,
			SessionCostUSD:   2.2901,
			SessionCostKnown: true,
			ModelUsage: []ui.ModelUsageStat{
				{Provider: "openrouter", Model: "mistral/7b", Mode: "agent", CostKnown: true, CostUSD: 2.2901},
				{Provider: "gemini-apikey", Model: "gemini-flash-latest", Mode: "agent", CostKnown: false},
			},
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	detail := stripANSI(status.Detail)
	right := stripANSI(status.Right)

	if !strings.Contains(detail, "Σ") {
		t.Errorf("footer Detail missing session Σ: %q", detail)
	}
	if strings.Contains(detail, "$") {
		t.Errorf("footer Detail should hide session $ on gemini after OpenRouter spend: %q", detail)
	}
	if strings.Contains(right, "$") {
		t.Errorf("footer Right should not show last-turn $ when LastCostKnown=false: %q", right)
	}
}

func TestFooterNoCostWhenNotKnown(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  100,
			LastOutputTokens: 40,
			InputTokens:      100,
			OutputTokens:     40,
			LastCostKnown:    false,
			SessionCostKnown: false,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()

	if strings.Contains(stripANSI(status.Right), "$") {
		t.Errorf("footer Right should not show cost when CostKnown=false: %q", status.Right)
	}
	if strings.Contains(stripANSI(status.Detail), "$") {
		t.Errorf("footer Detail should not show cost when CostKnown=false: %q", status.Detail)
	}
}

func TestFooterContextPercentStillShown(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  1000,
			LastOutputTokens: 400,
			ContextTokens:    5000,
			ContextLimit:     10000,
			Duration:         30 * time.Second,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	right := stripANSI(status.Right)

	if !strings.Contains(right, "50% ctx") {
		t.Errorf("footer Right missing context%% gauge: %q", right)
	}
}

func TestFooterDetailShowsSettingsNotHints(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  100,
			LastOutputTokens: 40,
			InputTokens:      100,
			OutputTokens:     40,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	status := m.statusWithMetrics()
	detail := stripANSI(status.Detail)
	for _, want := range []string{"screen: full", "thinking:", "mouse: on"} {
		if !strings.Contains(detail, want) {
			t.Errorf("footer Detail missing %q\n%s", want, detail)
		}
	}
	// The keys belong to the status row above the composer, not this row.
	for _, banned := range []string{"Ctrl+T", "Alt+M", "/screen"} {
		if strings.Contains(detail, banned) {
			t.Errorf("footer Detail repeats key hint %q\n%s", banned, detail)
		}
	}
}

func TestFooterSettingsFollowLiveState(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, quitApp{}, NewTerminal(ui.Options{}))

	if got := m.footerSettings(); got != "screen: full · thinking: hidden · mouse: on" {
		t.Fatalf("default settings = %q", got)
	}
	m.mouseEnabled = false
	m.showThinking, m.thinkingToggled = true, true
	if got := m.footerSettings(); got != "screen: full · thinking: shown · mouse: off" {
		t.Fatalf("toggled settings = %q", got)
	}
	m.inline = true
	if got := m.footerSettings(); got != "screen: inline · thinking: shown · mouse: terminal" {
		t.Fatalf("inline settings = %q", got)
	}
}

func TestFooterCachedTokensShownWhenReported(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:   1000,
			LastOutputTokens:  200,
			LastCachedTokens:  910,
			LastCacheReported: true,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	right := stripANSI(m.statusWithMetrics().Right)
	if !strings.Contains(right, "↑1.0k (91% cached) ↓200") {
		t.Errorf("footer Right missing cached percentage: %q", right)
	}

	// Narrow terminals omit the cached parenthetical to preserve space.
	m.width = 60
	narrowRight := stripANSI(m.statusWithMetrics().Right)
	if strings.Contains(narrowRight, "cached") {
		t.Errorf("narrow footer Right should omit cached parenthetical: %q", narrowRight)
	}
}

func TestFooterCachedTokensHiddenWhenUnreported(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:   1000,
			LastOutputTokens:  200,
			LastCachedTokens:  0,
			LastCacheReported: false,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	right := stripANSI(m.statusWithMetrics().Right)
	if strings.Contains(right, "cached") {
		t.Errorf("footer Right should not show cached when unreported: %q", right)
	}
}

func TestFooterTokensPerSecBeforeContext(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  1000,
			LastOutputTokens: 400,
			TokensPerSec:     42,
			ContextTokens:    5000,
			ContextLimit:     10000,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	right := stripANSI(m.statusWithMetrics().Right)
	speed := strings.Index(right, "42 tok/s")
	ctx := strings.Index(right, "50% ctx")
	if speed < 0 || ctx < 0 || speed > ctx {
		t.Errorf("tok/s should sit directly before %% ctx: %q", right)
	}
	if !strings.Contains(right, "↓400  ·  42 tok/s  ·  50% ctx") {
		t.Errorf("footer Right = %q, want speed between usage and context", right)
	}
}

func TestFooterTokensPerSecHiddenWhenZeroOrNarrow(t *testing.T) {
	t.Parallel()

	app := footerMetricsApp{
		stats: ui.SessionStats{
			LastInputTokens:  1000,
			LastOutputTokens: 400,
			TokensPerSec:     0,
			ContextTokens:    5000,
			ContextLimit:     10000,
		},
	}
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width = 100
	right := stripANSI(m.statusWithMetrics().Right)
	if strings.Contains(right, "tok/s") {
		t.Errorf("zero speed should be omitted: %q", right)
	}
	if !strings.Contains(right, "50% ctx") {
		t.Errorf("context gauge should remain: %q", right)
	}

	app.stats.TokensPerSec = 42
	m.width = 79
	narrow := stripANSI(m.statusWithMetrics().Right)
	if strings.Contains(narrow, "tok/s") {
		t.Errorf("narrow footer should omit tok/s: %q", narrow)
	}
	if !strings.Contains(narrow, "50% ctx") {
		t.Errorf("narrow footer should keep context gauge: %q", narrow)
	}
}
