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
	Provider      string
	Model         string
	Mode          string // "agent", "plan", "ask", "debug"
	Agent         string // "main", "subagent"
	Requests      int
	InTokens      int
	OutTokens     int
	CachedTokens  int
	CacheReported bool
	CostUSD       float64
	CostKnown     bool
}

// CacheBreakStat records one prompt-prefix break observed during the session,
// rendered by `/stats cache`.
type CacheBreakStat struct {
	Round             int
	Reason            string
	Position          int
	NextInputTokens   int
	NextCachedTokens  int
	NextCacheReported bool
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

	// InputTokens / OutputTokens / CachedTokens are cumulative session totals.
	// CacheReported is true when at least one request reported cached-token
	// details (distinguishing a real 0% hit rate from an unreported metric).
	InputTokens   int
	OutputTokens  int
	CachedTokens  int
	CacheReported bool

	// SessionCostUSD / SessionCostKnown are the cumulative session cost.
	// SessionCostKnown is true only when at least one request reported a cost
	// (currently only OpenRouter).
	SessionCostUSD   float64
	SessionCostKnown bool

	// LastInputTokens / LastOutputTokens / LastCachedTokens are the token counts
	// for the most recently completed main turn (not compression). Shown in the
	// footer next to the model label.
	LastInputTokens   int
	LastOutputTokens  int
	LastCachedTokens  int
	LastCacheReported bool

	// LastCostUSD / LastCostKnown are the cost for the last main turn.
	LastCostUSD   float64
	LastCostKnown bool

	// ContextTokens is the estimated size of the current context window and
	// ContextLimit its capacity (0 when no limit is known, e.g. off the
	// openai-chat path). ContextPercent derives the footer usage figure.
	ContextTokens int
	ContextLimit  int

	// TokensPerSec is the active turn's decode rate (generated tokens divided
	// by time from the first content chunk to the last). Zero means unknown:
	// the turn has not started, the provider buffered the reply into one chunk,
	// or the turn is still below the minimum size. The footer omits it then.
	TokensPerSec float64

	// Duration is the wall-clock session length.
	Duration time.Duration

	// ModelUsage breaks down token counts by provider+model+mode.
	// Empty when no generate calls have been recorded.
	ModelUsage []ModelUsageStat

	// CacheBreaks lists prompt-prefix breaks observed across main-agent rounds.
	CacheBreaks []CacheBreakStat
}

// ShowSessionCostInFooter is true when cumulative known cost exists and the
// active provider (Provider) has itself reported cost this session. Used by
// the live footer so a switch to Gemini (or any non-cost provider) hides the
// leftover OpenRouter $; switching back to a cost-reporting provider shows it
// again. Exit /stats keep using SessionCostKnown so attribution is unchanged.
func (s SessionStats) ShowSessionCostInFooter() bool {
	if !s.SessionCostKnown || s.Provider == "" {
		return false
	}
	for _, u := range s.ModelUsage {
		if u.CostKnown && u.Provider == s.Provider {
			return true
		}
	}
	return false
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
	// EscapeAtOnPaste indicates whether unescaped '@' should be auto-escaped to '\@' on paste.
	EscapeAtOnPaste bool
	// GoalActive is true if there is an autonomous goal currently active or paused.
	GoalActive bool
	// GoalStatusText summarizes the goal state (e.g. "Pursuing goal (3/25) — active").
	GoalStatusText string
	// GrillActive is true if there is a grill-me session currently active or paused.
	GrillActive bool
	// GrillStatusText summarizes the grill state (e.g. "Grill: 3 questions").
	GrillStatusText string
	// ReadOnlyPosture is true if the durable session-wide read-only posture is set.
	ReadOnlyPosture bool
	// TitleAnnouncementText announces an auto-generated session title in prompt
	// mode (e.g. `Named "Fix LSP pool race" — Ctrl+E rename`). It is rendered as
	// a passive status-row line that never blocks the composer; the TUI owns the
	// shown-once / dismissed lifecycle.
	TitleAnnouncementText string
	// ReasoningEffort is the explicit reasoning effort for the live model: the
	// session override (Alt+R, /reasoning <level>) or a pinned settings value.
	// Empty means the model or provider decides, so the footer omits it.
	ReasoningEffort string
	// SkillsDisabled is true when sagittarius.skillsEnabled is false. The footer
	// shows "skills: off" only then; when skills are on, the status row's skill
	// count already speaks for them.
	SkillsDisabled bool
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

// ReasoningController is an optional capability the TUI uses to step the
// session reasoning-effort override for the live model (Alt+R). level is the
// new override ("" means back to the model default); desc is a one-line
// message for the transcript. The override is never persisted. The agent App
// implements it.
type ReasoningController interface {
	CycleReasoningEffort() (level, desc string, err error)
}

// ApprovalController is an optional capability the TUI uses to toggle yolo
// tool approval for this session only (Alt+Y). mode is the approval mode now
// in effect and yolo reports whether it is yolo. The agent App implements it.
type ApprovalController interface {
	ToggleYolo() (mode string, yolo bool, err error)
}

// SubagentCanceller is an optional capability the TUI uses to stop one running
// subagent (Ctrl+X on its selected task-group card) without canceling its
// siblings or the turn. The argument is the tool call ID on the card. It
// returns false when no such child is running. The agent App implements it.
type SubagentCanceller interface {
	CancelSubagent(callID string) bool
}

// RequestDebugExporter is an optional capability the TUI uses to write the
// most recent provider request to a JSON file (Ctrl+Shift+D, same as /chat
// debug). Safe to call while a turn is in flight: the runner stores the
// request before the stream starts. The agent App implements it.
type RequestDebugExporter interface {
	ExportRequestDebug() (path string, err error)
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

// FormatTokensPerSec renders a decode rate for the footer. Rates under 10 keep
// one decimal (7.5 tok/s); larger rates are whole numbers (42 tok/s). Zero and
// negative rates return "" so the footer can omit an unknown figure.
func FormatTokensPerSec(tps float64) string {
	if tps <= 0 {
		return ""
	}
	if tps < 10 {
		return fmt.Sprintf("%.1f tok/s", tps)
	}
	return fmt.Sprintf("%.0f tok/s", tps)
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

// LastCacheHitPercent returns the cached-token share for the most recent turn
// (0–100), or -1 when the provider did not report cached tokens or no input
// tokens were recorded.
func (s SessionStats) LastCacheHitPercent() int {
	if !s.LastCacheReported || s.LastInputTokens <= 0 {
		return -1
	}
	return clampPercent(s.LastCachedTokens * 100 / s.LastInputTokens)
}

// FormatCacheHitPercent formats a cached-token share for tables, returning "-"
// when cached tokens were not reported or no input tokens were recorded.
func FormatCacheHitPercent(cached, in int, reported bool) string {
	if !reported || in <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", clampPercent(cached*100/in))
}

func clampPercent(pct int) int {
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

// ModelUsageRow is a per-model aggregation of ModelUsageStat across modes,
// used by both the exit screen and /stats.
type ModelUsageRow struct {
	Label         string // "{provider}/{model}", or bare model when provider is empty
	Requests      int
	InTokens      int
	OutTokens     int
	CachedTokens  int
	CacheReported bool
	CostUSD       float64
	CostKnown     bool
	Modes         []ModelUsageStat // sorted by Mode
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
		row.CachedTokens += s.CachedTokens
		if s.CacheReported {
			row.CacheReported = true
		}
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
		sort.Slice(row.Modes, func(i, j int) bool {
			if row.Modes[i].Agent != row.Modes[j].Agent {
				return row.Modes[i].Agent < row.Modes[j].Agent
			}
			return row.Modes[i].Mode < row.Modes[j].Mode
		})
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
// summary, "model" for the per-model token table, "tools" for tool-call counts,
// "cache" for prompt-cache hit rates and prefix breaks.
func FormatSessionStats(stats SessionStats, section string) string {
	switch section {
	case "model":
		return formatModelSection(stats)
	case "tools":
		return formatToolsSection(stats)
	case "cache":
		return formatCacheSection(stats)
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
	if stats.CacheReported && stats.InputTokens > 0 {
		rows = append(rows, [2]string{
			"Prompt cache",
			fmt.Sprintf("%s of %s input tokens (%s)",
				CompactCount(stats.CachedTokens),
				CompactCount(stats.InputTokens),
				FormatCacheHitPercent(stats.CachedTokens, stats.InputTokens, true)),
		})
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

// formatCacheSection renders the prompt-cache hit rate and the chronological
// list of prefix breaks observed during the session.
func formatCacheSection(stats SessionStats) string {
	var b strings.Builder
	switch {
	case stats.CacheReported && stats.InputTokens > 0:
		fmt.Fprintf(&b, "Prompt cache: %s of %s input tokens (%s)",
			CompactCount(stats.CachedTokens),
			CompactCount(stats.InputTokens),
			FormatCacheHitPercent(stats.CachedTokens, stats.InputTokens, true))
	case stats.InputTokens > 0:
		b.WriteString("Prompt cache: - (not reported by provider)")
	default:
		b.WriteString("Prompt cache: no requests recorded yet")
	}

	fmt.Fprintf(&b, "\nPrefix breaks: %d", len(stats.CacheBreaks))
	if len(stats.CacheBreaks) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "\n  %5s  %-22s  %8s  %8s", "Round", "Reason", "Position", "Next hit")
	for _, br := range stats.CacheBreaks {
		hit := FormatCacheHitPercent(br.NextCachedTokens, br.NextInputTokens, br.NextCacheReported)
		fmt.Fprintf(&b, "\n  %5d  %-22s  %8d  %8s", br.Round, br.Reason, br.Position, hit)
	}
	return b.String()
}

// formatModelTable renders the shared per-model/per-mode usage breakdown as
// plain text, appending a cost column only when any row carries a known cost.
func formatModelTable(stats []ModelUsageStat) string {
	rows, showCost := AggregateModelUsage(stats)
	var b strings.Builder
	b.WriteString("Model Usage")
	if showCost {
		fmt.Fprintf(&b, "\n  %-32s  %5s  %8s  %8s  %7s  %9s", "Model", "Reqs", "In", "Out", "Cached", "Cost")
	} else {
		fmt.Fprintf(&b, "\n  %-32s  %5s  %8s  %8s  %7s", "Model", "Reqs", "In", "Out", "Cached")
	}
	for _, row := range rows {
		writeModelTableRow(&b, "  "+row.Label, row.Requests, row.InTokens, row.OutTokens, row.CachedTokens, row.CacheReported, row.CostUSD, row.CostKnown, showCost)
		for _, mode := range row.Modes {
			lbl := mode.Mode
			if mode.Agent != "" && mode.Agent != "main" {
				lbl += " (" + mode.Agent + ")"
			}
			writeModelTableRow(&b, "    ↳ "+lbl, mode.Requests, mode.InTokens, mode.OutTokens, mode.CachedTokens, mode.CacheReported, mode.CostUSD, mode.CostKnown, showCost)
		}
	}
	return b.String()
}

// writeModelTableRow appends one aligned usage line (parent or mode child) to b.
func writeModelTableRow(b *strings.Builder, label string, reqs, in, out, cached int, cacheReported bool, costUSD float64, costKnown, showCost bool) {
	cachedStr := FormatCacheHitPercent(cached, in, cacheReported)
	if showCost {
		cost := ""
		if costKnown {
			cost = FormatCostUSD(costUSD)
		}
		fmt.Fprintf(b, "\n%-34s  %5d  %8s  %8s  %7s  %9s", label, reqs, CompactCount(in), CompactCount(out), cachedStr, cost)
		return
	}
	fmt.Fprintf(b, "\n%-34s  %5d  %8s  %8s  %7s", label, reqs, CompactCount(in), CompactCount(out), cachedStr)
}
