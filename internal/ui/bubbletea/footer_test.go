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
			LastInputTokens:  200,
			LastOutputTokens: 80,
			LastCostUSD:      0.0021,
			LastCostKnown:    true,
			InputTokens:      400,
			OutputTokens:     160,
			SessionCostUSD:   0.0042,
			SessionCostKnown: true,
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
