package bubbletea

import (
	"fmt"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// renderExitSummary builds the goodbye screen shown after the session ends: a
// title, interaction and performance stats, token usage, and a resume hint.
// It reads live telemetry from the app when available; with no metrics it falls
// back to the title and resume line only.
func (m *model) renderExitSummary() string {
	var sections []string
	sections = append(sections, m.th.Title.Render("Agent powering down. Goodbye!"))

	if mp, ok := m.app.(ui.MetricsProvider); ok {
		stats := mp.SessionMetrics()
		sections = append(sections, m.renderExitStats(stats))
		if hint := resumeHint(stats.SessionID); hint != "" {
			sections = append(sections, m.th.Secondary.Render(hint))
		}
	}

	return strings.Join(sections, "\n\n") + "\n"
}

func (m *model) renderExitStats(stats ui.SessionStats) string {
	label := m.th.Secondary
	rows := [][2]string{
		{"Session", strings.TrimSpace(stats.SessionID)},
		{"Provider", stats.Provider},
		{"Model", stats.Model},
		{"Turns", fmt.Sprintf("%d", stats.Turns)},
		{"Tool calls", toolCallsSummary(stats)},
		{"Duration", formatDuration(stats.Duration)},
		{"Tokens (in/out)", fmt.Sprintf("%s / %s", compactCount(stats.InputTokens), compactCount(stats.OutputTokens))},
	}

	var b strings.Builder
	for i, row := range rows {
		if row[1] == "" {
			continue
		}
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(label.Render(fmt.Sprintf("  %-16s ", row[0]+":")))
		b.WriteString(m.th.Primary.Render(row[1]))
	}
	return strings.TrimRight(b.String(), "\n")
}

func toolCallsSummary(stats ui.SessionStats) string {
	if stats.ToolCalls == 0 {
		return "0"
	}
	ok := stats.ToolCalls - stats.ToolFailures
	pct := ok * 100 / stats.ToolCalls
	return fmt.Sprintf("%d (%d%% ok)", stats.ToolCalls, pct)
}

func resumeHint(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return "To resume this session: sagittarius --resume " + sessionID
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.Round(time.Second).String()
}
