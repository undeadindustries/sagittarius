package ui

import (
	"strings"
	"testing"
	"time"
)

func TestCompactCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"zero", 0, "0"},
		{"sub-thousand", 999, "999"},
		{"exactly-1k", 1000, "1.0k"},
		{"rounded", 1234, "1.2k"},
		{"large", 45200, "45.2k"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CompactCount(tt.n); got != tt.want {
				t.Errorf("CompactCount(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

func TestFormatCostUSD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		usd  float64
		want string
	}{
		{"zero", 0, "$0.0000"},
		{"small", 0.0021, "$0.0021"},
		{"dollar", 1.2345, "$1.2345"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatCostUSD(tt.usd); got != tt.want {
				t.Errorf("FormatCostUSD(%v) = %q, want %q", tt.usd, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero-omitted", 0, ""},
		{"negative-omitted", -5 * time.Second, ""},
		{"rounded", 95 * time.Second, "1m35s"},
		{"sub-second-rounds-down", 400 * time.Millisecond, "0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FormatDuration(tt.d); got != tt.want {
				t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

func TestToolCallsSummary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		stats SessionStats
		want  string
	}{
		{"no-calls", SessionStats{ToolCalls: 0}, "0"},
		{"all-ok", SessionStats{ToolCalls: 3, ToolFailures: 0}, "3 (100% ok)"},
		{"partial-failure", SessionStats{ToolCalls: 4, ToolFailures: 1}, "4 (75% ok)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ToolCallsSummary(tt.stats); got != tt.want {
				t.Errorf("ToolCallsSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAggregateModelUsage(t *testing.T) {
	t.Parallel()

	t.Run("same-model-different-providers-stay-distinct", func(t *testing.T) {
		t.Parallel()
		stats := []ModelUsageStat{
			{Provider: "openrouter", Model: "gemini-2.5-pro", Mode: "agent", Requests: 1, InTokens: 100, OutTokens: 40},
			{Provider: "gemini", Model: "gemini-2.5-pro", Mode: "agent", Requests: 2, InTokens: 200, OutTokens: 80},
		}
		rows, showCost := AggregateModelUsage(stats)
		if showCost {
			t.Errorf("showCost = true, want false (no CostKnown)")
		}
		if len(rows) != 2 {
			t.Fatalf("got %d rows, want 2 (providers must not merge)", len(rows))
		}
		// Order is by provider+model key: "gemini" sorts before "openrouter".
		if rows[0].Label != "gemini/gemini-2.5-pro" {
			t.Errorf("rows[0].Label = %q, want gemini/gemini-2.5-pro", rows[0].Label)
		}
		if rows[1].Label != "openrouter/gemini-2.5-pro" {
			t.Errorf("rows[1].Label = %q, want openrouter/gemini-2.5-pro", rows[1].Label)
		}
	})

	t.Run("sums-cost-and-sorts-modes", func(t *testing.T) {
		t.Parallel()
		stats := []ModelUsageStat{
			{Provider: "openrouter", Model: "m", Mode: "plan", Requests: 1, InTokens: 80, OutTokens: 30, CostUSD: 0.0015, CostKnown: true},
			{Provider: "openrouter", Model: "m", Mode: "agent", Requests: 2, InTokens: 100, OutTokens: 40, CostUSD: 0.0021, CostKnown: true},
		}
		rows, showCost := AggregateModelUsage(stats)
		if !showCost {
			t.Errorf("showCost = false, want true")
		}
		if len(rows) != 1 {
			t.Fatalf("got %d rows, want 1", len(rows))
		}
		row := rows[0]
		if row.Requests != 3 || row.InTokens != 180 || row.OutTokens != 70 {
			t.Errorf("aggregation wrong: %+v", row)
		}
		if !row.CostKnown {
			t.Errorf("CostKnown = false, want true")
		}
		if got := row.CostUSD; got < 0.0035 || got > 0.0037 {
			t.Errorf("CostUSD = %v, want ~0.0036", got)
		}
		// Modes sorted ascending by Mode: agent before plan.
		if len(row.Modes) != 2 || row.Modes[0].Mode != "agent" || row.Modes[1].Mode != "plan" {
			t.Errorf("modes not sorted: %+v", row.Modes)
		}
	})

	t.Run("empty-provider-uses-bare-model", func(t *testing.T) {
		t.Parallel()
		rows, _ := AggregateModelUsage([]ModelUsageStat{
			{Provider: "  ", Model: "bare-model", Mode: "agent", Requests: 1},
		})
		if len(rows) != 1 || rows[0].Label != "bare-model" {
			t.Errorf("bare-model label wrong: %+v", rows)
		}
	})
}

func TestFormatSessionStats(t *testing.T) {
	t.Parallel()

	full := SessionStats{
		SessionID: "abcdef1234567890",
		Provider:  "OpenAI",
		Model:     "gpt-5-codex",
		Turns:     3,
		ToolCalls: 4, ToolFailures: 1,
		InputTokens: 12000, OutputTokens: 3400,
		Duration: 95 * time.Second,
		ModelUsage: []ModelUsageStat{
			{Provider: "openrouter", Model: "mistral/7b", Mode: "agent", Requests: 1, InTokens: 100, OutTokens: 40, CostUSD: 0.0021, CostKnown: true},
			{Provider: "openrouter", Model: "mistral/7b", Mode: "plan", Requests: 1, InTokens: 80, OutTokens: 30, CostUSD: 0.0015, CostKnown: true},
		},
		SessionCostUSD: 0.0036, SessionCostKnown: true,
	}

	t.Run("session-section", func(t *testing.T) {
		t.Parallel()
		out := FormatSessionStats(full, "session")
		for _, want := range []string{
			"Session statistics",
			"OpenAI",
			"gpt-5-codex",
			"Turns:",
			"4 (75% ok)",
			"Model Usage",
			"mistral/7b",
			"↳ agent",
			"$0.0021",
			"Session cost:",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("session output missing %q\n%s", want, out)
			}
		}
	})

	t.Run("default-empty-section-is-session", func(t *testing.T) {
		t.Parallel()
		if FormatSessionStats(full, "") != FormatSessionStats(full, "session") {
			t.Errorf("empty section should match session section")
		}
		if FormatSessionStats(full, "bogus") != FormatSessionStats(full, "session") {
			t.Errorf("unrecognized section should fall back to session section")
		}
	})

	t.Run("session-fallback-flat-tokens", func(t *testing.T) {
		t.Parallel()
		s := SessionStats{Provider: "OpenAI", Model: "m", InputTokens: 12000, OutputTokens: 3400}
		out := FormatSessionStats(s, "session")
		if !strings.Contains(out, "Tokens (in/out):") || !strings.Contains(out, "12.0k / 3.4k") {
			t.Errorf("missing flat token fallback\n%s", out)
		}
		if strings.Contains(out, "Model Usage") {
			t.Errorf("should not render model table when ModelUsage empty\n%s", out)
		}
	})

	t.Run("model-section", func(t *testing.T) {
		t.Parallel()
		out := FormatSessionStats(full, "model")
		if !strings.Contains(out, "Model Usage") || !strings.Contains(out, "Cost") {
			t.Errorf("model section missing table/cost\n%s", out)
		}
	})

	t.Run("model-section-empty", func(t *testing.T) {
		t.Parallel()
		out := FormatSessionStats(SessionStats{}, "model")
		if out != "No model usage recorded yet." {
			t.Errorf("model empty-state = %q", out)
		}
	})

	t.Run("tools-section", func(t *testing.T) {
		t.Parallel()
		out := FormatSessionStats(full, "tools")
		for _, want := range []string{"Tool calls: 4", "Failures: 1", "Success rate: 75%"} {
			if !strings.Contains(out, want) {
				t.Errorf("tools section missing %q\n%s", want, out)
			}
		}
	})

	t.Run("tools-section-empty", func(t *testing.T) {
		t.Parallel()
		out := FormatSessionStats(SessionStats{}, "tools")
		if out != "No tool calls recorded yet." {
			t.Errorf("tools empty-state = %q", out)
		}
	})
}
