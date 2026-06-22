package bubbletea

import (
	"fmt"
	"sort"
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

	if len(stats.ModelUsage) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.renderModelUsage(stats.ModelUsage))
	} else {
		// Fallback: flat token row when no per-model data is available.
		tok := fmt.Sprintf("%s / %s", compactCount(stats.InputTokens), compactCount(stats.OutputTokens))
		b.WriteString("\n")
		b.WriteString(label.Render(fmt.Sprintf("  %-16s ", "Tokens (in/out):")))
		b.WriteString(m.th.Primary.Render(tok))
	}

	return strings.TrimRight(b.String(), "\n")
}

// renderModelUsage builds a Gemini-style breakdown grouped by model, with
// child rows per process type (main / compression).
//
//	Model Usage
//
//	  gemini-2.5-pro           3   45.2k   1.1k
//	    ↳ main                 3   45.2k   1.1k
//	  gemini-1.5-flash         1    8.0k    300
//	    ↳ compression          1    8.0k    300
func (m *model) renderModelUsage(stats []ui.ModelUsageStat) string {
	// Collect unique models in stable order.
	type modelRow struct {
		model     string
		requests  int
		inTokens  int
		outTokens int
		kinds     []ui.ModelUsageStat // rows for this model
	}

	byModel := map[string]*modelRow{}
	modelOrder := []string{}
	for _, s := range stats {
		mr := byModel[s.Model]
		if mr == nil {
			mr = &modelRow{model: s.Model}
			byModel[s.Model] = mr
			modelOrder = append(modelOrder, s.Model)
		}
		mr.requests += s.Requests
		mr.inTokens += s.InTokens
		mr.outTokens += s.OutTokens
		mr.kinds = append(mr.kinds, s)
	}
	sort.Strings(modelOrder)

	label := m.th.Secondary
	dim := m.th.Dim
	primary := m.th.Primary

	var b strings.Builder
	b.WriteString(label.Render("  Model Usage"))
	b.WriteString("\n")
	b.WriteString(label.Render(fmt.Sprintf("  %-32s  %4s  %7s  %7s", "Model", "Reqs", "In", "Out")))

	for _, mod := range modelOrder {
		mr := byModel[mod]
		b.WriteString("\n")
		b.WriteString(primary.Render(fmt.Sprintf("  %-32s  %4d  %7s  %7s",
			truncate(mod, 32),
			mr.requests,
			compactCount(mr.inTokens),
			compactCount(mr.outTokens),
		)))

		// Sort kinds for stable output.
		sort.Slice(mr.kinds, func(i, j int) bool { return mr.kinds[i].Kind < mr.kinds[j].Kind })
		for _, k := range mr.kinds {
			b.WriteString("\n")
			b.WriteString(dim.Render(fmt.Sprintf("    ↳ %-28s  %4d  %7s  %7s",
				k.Kind,
				k.Requests,
				compactCount(k.InTokens),
				compactCount(k.OutTokens),
			)))
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
