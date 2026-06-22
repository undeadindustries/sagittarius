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
	sessionCostStr := ""
	if stats.SessionCostKnown {
		sessionCostStr = formatCostUSD(stats.SessionCostUSD)
	}
	rows := [][2]string{
		{"Session", strings.TrimSpace(stats.SessionID)},
		{"Provider", stats.Provider},
		{"Model", stats.Model},
		{"Turns", fmt.Sprintf("%d", stats.Turns)},
		{"Tool calls", toolCallsSummary(stats)},
		{"Duration", formatDuration(stats.Duration)},
		{"Session cost", sessionCostStr},
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
	// Collect unique (provider, model) pairs in stable order. Metrics are keyed
	// by (provider, model, mode), so grouping by model id alone would merge two
	// providers that expose the same model id (e.g. gemini-native and openrouter
	// both serving a gemini-* id) into one row with summed tokens/cost.
	type modelRow struct {
		label     string
		requests  int
		inTokens  int
		outTokens int
		costUSD   float64
		costKnown bool
		modes     []ui.ModelUsageStat
	}

	// Determine whether any row has cost data to decide whether to show the column.
	showCost := false
	for _, s := range stats {
		if s.CostKnown {
			showCost = true
			break
		}
	}

	byModel := map[string]*modelRow{}
	modelOrder := []string{}
	for _, s := range stats {
		key := s.Provider + "\x00" + s.Model
		mr := byModel[key]
		if mr == nil {
			mr = &modelRow{label: modelUsageLabel(s.Provider, s.Model)}
			byModel[key] = mr
			modelOrder = append(modelOrder, key)
		}
		mr.requests += s.Requests
		mr.inTokens += s.InTokens
		mr.outTokens += s.OutTokens
		mr.costUSD += s.CostUSD
		if s.CostKnown {
			mr.costKnown = true
		}
		mr.modes = append(mr.modes, s)
	}
	sort.Strings(modelOrder)

	label := m.th.Secondary
	dim := m.th.Dim
	primary := m.th.Primary

	var b strings.Builder
	b.WriteString(label.Render("  Model Usage"))
	b.WriteString("\n")
	if showCost {
		b.WriteString(label.Render(fmt.Sprintf("  %-32s  %4s  %7s  %7s  %9s", "Model", "Reqs", "In", "Out", "Cost")))
	} else {
		b.WriteString(label.Render(fmt.Sprintf("  %-32s  %4s  %7s  %7s", "Model", "Reqs", "In", "Out")))
	}

	for _, key := range modelOrder {
		mr := byModel[key]
		b.WriteString("\n")
		if showCost {
			costStr := ""
			if mr.costKnown {
				costStr = formatCostUSD(mr.costUSD)
			}
			b.WriteString(primary.Render(fmt.Sprintf("  %-32s  %4d  %7s  %7s  %9s",
				truncate(mr.label, 32),
				mr.requests,
				compactCount(mr.inTokens),
				compactCount(mr.outTokens),
				costStr,
			)))
		} else {
			b.WriteString(primary.Render(fmt.Sprintf("  %-32s  %4d  %7s  %7s",
				truncate(mr.label, 32),
				mr.requests,
				compactCount(mr.inTokens),
				compactCount(mr.outTokens),
			)))
		}

		// Sort modes for stable output.
		sort.Slice(mr.modes, func(i, j int) bool { return mr.modes[i].Mode < mr.modes[j].Mode })
		for _, k := range mr.modes {
			b.WriteString("\n")
			if showCost {
				childCost := ""
				if k.CostKnown {
					childCost = formatCostUSD(k.CostUSD)
				}
				b.WriteString(dim.Render(fmt.Sprintf("    ↳ %-28s  %4d  %7s  %7s  %9s",
					k.Mode,
					k.Requests,
					compactCount(k.InTokens),
					compactCount(k.OutTokens),
					childCost,
				)))
			} else {
				b.WriteString(dim.Render(fmt.Sprintf("    ↳ %-28s  %4d  %7s  %7s",
					k.Mode,
					k.Requests,
					compactCount(k.InTokens),
					compactCount(k.OutTokens),
				)))
			}
		}
	}

	return b.String()
}

// modelUsageLabel renders a {provider}/{model} label for the usage table,
// falling back to the bare model id when the provider is unknown. This keeps
// same-named models from different providers visually distinct.
func modelUsageLabel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return model
	}
	return provider + "/" + model
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
