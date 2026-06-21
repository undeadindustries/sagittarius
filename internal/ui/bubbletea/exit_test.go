package bubbletea

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// metricsApp is a quitApp that also reports session metrics.
type metricsApp struct {
	quitApp
	stats ui.SessionStats
}

func (a metricsApp) SessionMetrics() ui.SessionStats { return a.stats }

func sampleStats() ui.SessionStats {
	return ui.SessionStats{
		SessionID:    "abcdef1234567890",
		Provider:     "OpenAI",
		Model:        "gpt-5-codex",
		Turns:        3,
		ToolCalls:    4,
		ToolFailures: 1,
		InputTokens:  12000,
		OutputTokens: 3400,
		Duration:     95 * time.Second,
	}
}

func TestExitSummaryShowsStatsAndResume(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale"}, metricsApp{stats: sampleStats()}, NewTerminal(ui.Options{}))
	out := stripANSI(m.renderExitSummary())

	for _, want := range []string{
		"Agent powering down. Goodbye!",
		"Session:",
		"abcdef12", // short id
		"OpenAI",
		"gpt-5-codex",
		"4 (75% ok)",
		"1m35s",
		"12.0k / 3.4k",
		"sagittarius --resume abcdef1234567890",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("exit summary missing %q\n%s", want, out)
		}
	}
}

func TestBeginQuitCapturesSummary(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, metricsApp{stats: sampleStats()}, NewTerminal(ui.Options{}))
	cmd := m.beginQuit()
	if cmd == nil {
		t.Fatal("beginQuit should return a quit command")
	}
	if !m.quitting {
		t.Fatal("beginQuit should set quitting")
	}
	if m.exitSummary == "" {
		t.Fatal("beginQuit should capture an exit summary")
	}
	// A second call must not overwrite the captured summary.
	first := m.exitSummary
	m.beginQuit()
	if m.exitSummary != first {
		t.Fatal("beginQuit should keep the first captured summary")
	}
}

func TestExitSummaryGreyscaleNoColor(t *testing.T) {
	t.Parallel()
	lipgloss.SetColorProfile(termenv.TrueColor)
	m := newModel(ui.Options{ThemeName: "greyscale"}, metricsApp{stats: sampleStats()}, NewTerminal(ui.Options{}))
	if ansiColorCode.MatchString(m.renderExitSummary()) {
		t.Error("greyscale exit summary emitted color codes")
	}
}

func TestQuitViewIsEmptyForCleanTeardown(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{}, metricsApp{stats: sampleStats()}, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if v := m.View(); v != "" {
		t.Errorf("View() should be empty while quitting (printed post-teardown), got %q", v)
	}
	_ = context.Background()
}
