package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModelUsageStat holds token counts (and optional cost) for one
// (provider, model, mode) triple observed during the session.
type ModelUsageStat struct {
	Provider  string
	Model     string
	Mode      string // "agent", "plan", "ask", "debug"
	Requests  int
	InTokens  int
	OutTokens int
	CostUSD   float64
	CostKnown bool
}

// SessionStats is a UI-facing snapshot of session telemetry. It carries no
// provider types so the agent/UI seam (AD-004) stays clean: the agent layer
// fills it, the bubbletea footer and exit screen render it.
type SessionStats struct {
	SessionID string
	Provider  string
	Model     string

	Turns        int
	ToolCalls    int
	ToolFailures int

	// InputTokens / OutputTokens are cumulative session totals.
	InputTokens  int
	OutputTokens int

	// SessionCostUSD / SessionCostKnown are the cumulative session cost.
	// SessionCostKnown is true only when at least one request reported a cost
	// (currently only OpenRouter).
	SessionCostUSD   float64
	SessionCostKnown bool

	// LastInputTokens / LastOutputTokens are the token counts for the most
	// recently completed main turn (not compression). Shown in the footer
	// next to the model label.
	LastInputTokens  int
	LastOutputTokens int

	// LastCostUSD / LastCostKnown are the cost for the last main turn.
	LastCostUSD   float64
	LastCostKnown bool

	// ContextTokens is the estimated size of the current context window and
	// ContextLimit its capacity (0 when no limit is known, e.g. off the
	// openai-chat path). ContextPercent derives the footer usage figure.
	ContextTokens int
	ContextLimit  int

	// Duration is the wall-clock session length.
	Duration time.Duration

	// ModelUsage breaks down token counts by provider+model+mode.
	// Empty when no generate calls have been recorded.
	ModelUsage []ModelUsageStat
}

// ContextPercent returns the share of the context window in use (0–100), or -1
// when no limit is known so callers can omit the figure.
func (s SessionStats) ContextPercent() int {
	if s.ContextLimit <= 0 {
		return -1
	}
	pct := s.ContextTokens * 100 / s.ContextLimit
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// MetricsProvider is an optional capability the TUI uses to read live session
// telemetry for the footer and exit summary. The agent App implements it.
type MetricsProvider interface {
	SessionMetrics() SessionStats
}

// ComposerStatus is optional metadata rendered in the composer status row
// (between the scrollback and the input box): the active tool-approval policy
// and a count of loaded skills. The AGENTS.md count is sourced separately from
// Options.LoadedMemoryFiles.
type ComposerStatus struct {
	// ApprovalMode is the tool approval policy ("default", "autoEdit", "yolo").
	ApprovalMode string
	// SkillCount is the number of discovered skills available to the agent.
	SkillCount int
	// ShowThinking is the resolved thinking-box visibility for the active
	// (provider, model): per-model override, else provider, else global setting.
	ShowThinking bool
	// GoalActive is true if there is an autonomous goal currently active or paused.
	GoalActive bool
	// GoalStatusText summarizes the goal state (e.g. "Pursuing goal (3/25) — active").
	GoalStatusText string
	// GrillActive is true if there is a grill-me session currently active or paused.
	GrillActive bool
	// GrillStatusText summarizes the grill state (e.g. "Grill: 3 questions").
	GrillStatusText string
}

// ComposerStatusProvider is an optional capability the TUI uses to render the
// composer status row. The agent App implements it.
type ComposerStatusProvider interface {
	ComposerStatus() ComposerStatus
}

// ThinkingController is an optional capability the TUI uses to persist the
// global thinking-box visibility when the user toggles it live (Ctrl+T). The
// agent App implements it.
type ThinkingController interface {
	SetShowThinking(on bool) error
}

// ThemeController is an optional capability the TUI uses to cycle the active
// color theme live (Alt+T) and persist the choice. CycleTheme returns the new
// canonical theme name ("default" or "greyscale"). The agent App implements it.
type ThemeController interface {
	CycleTheme() (string, error)
}

// CompactCount formats a token count compactly (e.g. 1234 -> "1.2k").
func CompactCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// FormatCostUSD renders a USD cost value to 4 significant decimal places
// (e.g. $0.0021, $1.2345).
func FormatCostUSD(usd float64) string {
	return fmt.Sprintf("$%.4f", usd)
}

// FormatDuration renders a session duration rounded to whole seconds, returning
// "" for non-positive durations so callers can omit the field.
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.Round(time.Second).String()
}

// ToolCallsSummary renders the tool-call count with a success percentage
// (e.g. "4 (75% ok)"), or "0" when no tools were called.
func ToolCallsSummary(stats SessionStats) string {
	if stats.ToolCalls == 0 {
		return "0"
	}
	ok := stats.ToolCalls - stats.ToolFailures
	pct := ok * 100 / stats.ToolCalls
	return fmt.Sprintf("%d (%d%% ok)", stats.ToolCalls, pct)
}

// ModelUsageRow is a per-model aggregation of ModelUsageStat across modes,
// used by both the exit screen and /stats.
type ModelUsageRow struct {
	Label     string // "{provider}/{model}", or bare model when provider is empty
	Requests  int
	InTokens  int
	OutTokens int
	CostUSD   float64
	CostKnown bool
	Modes     []ModelUsageStat // sorted by Mode
}

// AggregateModelUsage groups raw per-(provider,model,mode) stats into per-model
// rows sorted by the provider+model key, and reports whether any row carries a
// known cost (so callers can decide whether to render a cost column).
//
// Rows are keyed by provider+model (never model alone) so two providers exposing
// the same model id stay distinct rather than merging their tokens and cost.
func AggregateModelUsage(stats []ModelUsageStat) (rows []ModelUsageRow, showCost bool) {
	byKey := map[string]*ModelUsageRow{}
	order := make([]string, 0, len(stats))
	for _, s := range stats {
		if s.CostKnown {
			showCost = true
		}
		key := s.Provider + "\x00" + s.Model
		row := byKey[key]
		if row == nil {
			row = &ModelUsageRow{Label: modelUsageLabel(s.Provider, s.Model)}
			byKey[key] = row
			order = append(order, key)
		}
		row.Requests += s.Requests
		row.InTokens += s.InTokens
		row.OutTokens += s.OutTokens
		row.CostUSD += s.CostUSD
		if s.CostKnown {
			row.CostKnown = true
		}
		row.Modes = append(row.Modes, s)
	}
	sort.Strings(order)
	rows = make([]ModelUsageRow, 0, len(order))
	for _, key := range order {
		row := byKey[key]
		sort.Slice(row.Modes, func(i, j int) bool { return row.Modes[i].Mode < row.Modes[j].Mode })
		rows = append(rows, *row)
	}
	return rows, showCost
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

// FormatSessionStats renders session telemetry as plain text (no ANSI) for the
// /stats command. section selects the block: "" or "session" for the full
// summary, "model" for the per-model token table, "tools" for tool-call counts.
func FormatSessionStats(stats SessionStats, section string) string {
	switch section {
	case "model":
		return formatModelSection(stats)
	case "tools":
		return formatToolsSection(stats)
	default:
		return formatSessionSection(stats)
	}
}

// formatSessionSection renders the full session summary: a header, aligned
// key/value lines (empty values omitted), and the per-model usage table.
func formatSessionSection(stats SessionStats) string {
	var b strings.Builder
	b.WriteString("Session statistics")

	rows := [][2]string{
		{"Session", strings.TrimSpace(stats.SessionID)},
		{"Provider", stats.Provider},
		{"Model", stats.Model},
		{"Turns", fmt.Sprintf("%d", stats.Turns)},
		{"Tool calls", ToolCallsSummary(stats)},
		{"Duration", FormatDuration(stats.Duration)},
	}
	if stats.SessionCostKnown {
		rows = append(rows, [2]string{"Session cost", FormatCostUSD(stats.SessionCostUSD)})
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		fmt.Fprintf(&b, "\n  %-14s %s", row[0]+":", row[1])
	}

	if len(stats.ModelUsage) > 0 {
		b.WriteString("\n\n")
		b.WriteString(formatModelTable(stats.ModelUsage))
	} else {
		fmt.Fprintf(&b, "\n  %-14s %s / %s", "Tokens (in/out):",
			CompactCount(stats.InputTokens), CompactCount(stats.OutputTokens))
	}
	return b.String()
}

// formatModelSection renders only the per-model usage table, or an empty-state
// message when no usage has been recorded.
func formatModelSection(stats SessionStats) string {
	if len(stats.ModelUsage) == 0 {
		return "No model usage recorded yet."
	}
	return formatModelTable(stats.ModelUsage)
}

// formatToolsSection renders aggregate tool-call counts and the success rate, or
// an empty-state message when no tools have been called.
func formatToolsSection(stats SessionStats) string {
	if stats.ToolCalls == 0 {
		return "No tool calls recorded yet."
	}
	ok := stats.ToolCalls - stats.ToolFailures
	pct := ok * 100 / stats.ToolCalls
	var b strings.Builder
	fmt.Fprintf(&b, "Tool calls: %d", stats.ToolCalls)
	fmt.Fprintf(&b, "\nFailures: %d", stats.ToolFailures)
	fmt.Fprintf(&b, "\nSuccess rate: %d%%", pct)
	return b.String()
}

// formatModelTable renders the shared per-model/per-mode usage breakdown as
// plain text, appending a cost column only when any row carries a known cost.
func formatModelTable(stats []ModelUsageStat) string {
	rows, showCost := AggregateModelUsage(stats)
	var b strings.Builder
	b.WriteString("Model Usage")
	if showCost {
		fmt.Fprintf(&b, "\n  %-32s  %5s  %8s  %8s  %9s", "Model", "Reqs", "In", "Out", "Cost")
	} else {
		fmt.Fprintf(&b, "\n  %-32s  %5s  %8s  %8s", "Model", "Reqs", "In", "Out")
	}
	for _, row := range rows {
		writeModelTableRow(&b, "  "+row.Label, row.Requests, row.InTokens, row.OutTokens, row.CostUSD, row.CostKnown, showCost)
		for _, mode := range row.Modes {
			writeModelTableRow(&b, "    ↳ "+mode.Mode, mode.Requests, mode.InTokens, mode.OutTokens, mode.CostUSD, mode.CostKnown, showCost)
		}
	}
	return b.String()
}

// writeModelTableRow appends one aligned usage line (parent or mode child) to b.
func writeModelTableRow(b *strings.Builder, label string, reqs, in, out int, costUSD float64, costKnown, showCost bool) {
	if showCost {
		cost := ""
		if costKnown {
			cost = FormatCostUSD(costUSD)
		}
		fmt.Fprintf(b, "\n%-34s  %5d  %8s  %8s  %9s", label, reqs, CompactCount(in), CompactCount(out), cost)
		return
	}
	fmt.Fprintf(b, "\n%-34s  %5d  %8s  %8s", label, reqs, CompactCount(in), CompactCount(out))
}
