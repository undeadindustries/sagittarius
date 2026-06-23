package bubbletea

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// renderExitSummary builds the goodbye screen shown after the session ends: a
// title, interaction and performance stats, token usage, and a resume hint.
// It reads live telemetry from the app when available; with no metrics it falls
// back to the title and resume line only.
func (m *model) renderExitSummary() string {
	var sections []string
	titleText := "Agent powering down. Goodbye!"
	if len(m.th.TitleGradient) > 0 {
		sections = append(sections, m.th.GradientText(titleText, m.th.Title, m.th.TitleGradient))
	} else {
		sections = append(sections, m.th.Title.Render(titleText))
	}

	if mp, ok := m.app.(ui.MetricsProvider); ok {
		stats := mp.SessionMetrics()
		sections = append(sections, m.renderExitStats(stats))
		if hint := resumeHint(stats.SessionID); hint != "" {
			if len(m.th.TitleGradient) > 0 {
				sections = append(sections, m.th.GradientText(hint, m.th.Secondary, m.th.TitleGradient))
			} else {
				sections = append(sections, m.th.Secondary.Render(hint))
			}
		}
	}

	return strings.Join(sections, "\n\n") + "\n"
}

// statRow renders a single label+value row aligned in a gutter layout.
// label is blue (Link), value is white (Primary).
func (m *model) statRow(label, value string) string {
	return m.th.Link.Render(fmt.Sprintf("  %-18s", label+":")) + m.th.Primary.Render(value)
}

// sectionHeader renders a bold white section heading.
func (m *model) sectionHeader(title string) string {
	return m.th.Primary.Bold(true).Render(title)
}

func (m *model) renderExitStats(stats ui.SessionStats) string {
	var b strings.Builder

	// ── Interaction Summary ─────────────────────────────────────────────────
	b.WriteString(m.sectionHeader("Interaction Summary"))
	writeRow := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString("\n")
		b.WriteString(m.statRow(label, value))
	}
	writeRow("Session", strings.TrimSpace(stats.SessionID))
	writeRow("Provider", stats.Provider)
	writeRow("Model", stats.Model)
	writeRow("Turns", fmt.Sprintf("%d", stats.Turns))

	// Tool calls with semantic color indicators.
	if stats.ToolCalls > 0 {
		ok := stats.ToolCalls - stats.ToolFailures
		b.WriteString("\n")
		b.WriteString(m.th.Link.Render(fmt.Sprintf("  %-18s", "Tool calls:")))
		b.WriteString(m.th.Primary.Render(fmt.Sprintf("%d (", stats.ToolCalls)))
		b.WriteString(m.th.Success.Render(fmt.Sprintf("✓ %d", ok)))
		b.WriteString(m.th.Primary.Render(" "))
		b.WriteString(m.th.Error.Render(fmt.Sprintf("✗ %d", stats.ToolFailures)))
		b.WriteString(m.th.Primary.Render(")"))
	} else {
		writeRow("Tool calls", "0")
	}

	// ── Performance ─────────────────────────────────────────────────────────
	b.WriteString("\n\n")
	b.WriteString(m.sectionHeader("Performance"))
	writeRow("Duration", ui.FormatDuration(stats.Duration))
	if stats.SessionCostKnown {
		writeRow("Session cost", ui.FormatCostUSD(stats.SessionCostUSD))
	}

	// ── Token Usage ─────────────────────────────────────────────────────────
	if len(stats.ModelUsage) > 0 {
		b.WriteString("\n")
		b.WriteString(m.renderModelUsage(stats.ModelUsage))
	} else if stats.InputTokens > 0 || stats.OutputTokens > 0 {
		tok := fmt.Sprintf("%s / %s", ui.CompactCount(stats.InputTokens), ui.CompactCount(stats.OutputTokens))
		b.WriteString("\n\n")
		b.WriteString(m.sectionHeader("Token Usage"))
		b.WriteString("\n")
		b.WriteString(m.statRow("In / Out", tok))
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderModelUsage builds a per-model, per-mode breakdown for the exit screen.
//
//	Model Usage
//
//	  gemini-2.5-pro           3   45.2k   1.1k
//	    ↳ agent                3   45.2k   1.1k
//	  openai/gpt-4o            2    8.0k    300   $0.0042
//	    ↳ plan                 1    4.0k    150   $0.0021
//	    ↳ agent                1    4.0k    150   $0.0021
//
// The cost column is appended only when at least one row has CostKnown=true
// (i.e. the session used OpenRouter for at least one request).
func (m *model) renderModelUsage(stats []ui.ModelUsageStat) string {
	// Grouping (provider+model keying, mode sorting, cost detection) is shared
	// with /stats via ui.AggregateModelUsage; this function only themes the
	// rendering of the pre-aggregated rows.
	rows, showCost := ui.AggregateModelUsage(stats)

	dim := m.th.Dim
	primary := m.th.Primary

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(m.sectionHeader("Model Usage"))
	b.WriteString("\n")
	col := m.th.Secondary
	if showCost {
		b.WriteString(col.Render(fmt.Sprintf("  %-32s  %4s  %7s  %7s  %9s", "Model", "Reqs", "In", "Out", "Cost")))
	} else {
		b.WriteString(col.Render(fmt.Sprintf("  %-32s  %4s  %7s  %7s", "Model", "Reqs", "In", "Out")))
	}

	for _, row := range rows {
		b.WriteString("\n")
		if showCost {
			costStr := ""
			if row.CostKnown {
				costStr = ui.FormatCostUSD(row.CostUSD)
			}
			b.WriteString(primary.Render(fmt.Sprintf("  %-32s  %4d  %7s  %7s  %9s",
				truncate(row.Label, 32),
				row.Requests,
				ui.CompactCount(row.InTokens),
				ui.CompactCount(row.OutTokens),
				costStr,
			)))
		} else {
			b.WriteString(primary.Render(fmt.Sprintf("  %-32s  %4d  %7s  %7s",
				truncate(row.Label, 32),
				row.Requests,
				ui.CompactCount(row.InTokens),
				ui.CompactCount(row.OutTokens),
			)))
		}

		for _, k := range row.Modes {
			b.WriteString("\n")
			if showCost {
				childCost := ""
				if k.CostKnown {
					childCost = ui.FormatCostUSD(k.CostUSD)
				}
				b.WriteString(dim.Render(fmt.Sprintf("    ↳ %-28s  %4d  %7s  %7s  %9s",
					k.Mode,
					k.Requests,
					ui.CompactCount(k.InTokens),
					ui.CompactCount(k.OutTokens),
					childCost,
				)))
			} else {
				b.WriteString(dim.Render(fmt.Sprintf("    ↳ %-28s  %4d  %7s  %7s",
					k.Mode,
					k.Requests,
					ui.CompactCount(k.InTokens),
					ui.CompactCount(k.OutTokens),
				)))
			}
		}
	}

	return b.String()
}

// truncate clips s to at most n runes, appending "…" when clipped.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func resumeHint(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return "To resume this session: sagittarius --resume " + sessionID
}
