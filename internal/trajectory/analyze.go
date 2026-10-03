package trajectory

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Analysis holds the comprehensive diagnostics and ranked findings for an ATIF trajectory.
type Analysis struct {
	SessionID   string          `json:"session_id"`
	AgentName   string          `json:"agent_name"`
	ModelName   string          `json:"model_name"`
	TotalSteps  int             `json:"total_steps"`
	TotalTokens int             `json:"total_tokens"`
	TotalCost   float64         `json:"total_cost"`
	Efficiency  EfficiencyStats `json:"efficiency"`
	ToolHealth  ToolHealthStats `json:"tool_health"`
	Findings    []Finding       `json:"findings"`
}

type EfficiencyStats struct {
	UserTurns       int     `json:"user_turns"`
	AgentSteps      int     `json:"agent_steps"`
	LLMCalls        int     `json:"llm_calls"`
	StepsPerTurn    float64 `json:"steps_per_turn"`
	TotalTokens     int     `json:"total_tokens"`
	PromptTokens    int     `json:"prompt_tokens"`
	CompTokens      int     `json:"completion_tokens"`
	CachedTokens    int     `json:"cached_tokens"`
	CacheHitRatio   float64 `json:"cache_hit_ratio"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
}

type ToolHealthStats struct {
	TotalCalls   int            `json:"total_calls"`
	SuccessCalls int            `json:"success_calls"`
	FailedCalls  int            `json:"failed_calls"`
	FailureRate  float64        `json:"failure_rate"`
	ToolCounts   map[string]int `json:"tool_counts"`
	CodeCounts   map[string]int `json:"code_counts"`
}

type FindingSeverity string

const (
	SeverityInfo     FindingSeverity = "info"
	SeverityWarning  FindingSeverity = "warning"
	SeverityCritical FindingSeverity = "critical"
)

type Finding struct {
	Category string          `json:"category"`
	Severity FindingSeverity `json:"severity"`
	Title    string          `json:"title"`
	Detail   string          `json:"detail"`
	Remedy   string          `json:"remedy"`
}

func observationFor(step Step, callID string) (ObservationResult, bool) {
	if step.Observation == nil || callID == "" {
		return ObservationResult{}, false
	}
	for _, res := range step.Observation.Results {
		if res.SourceCallID == callID {
			return res, true
		}
	}
	return ObservationResult{}, false
}

func extraInt(extra map[string]any, key string) int {
	if extra == nil {
		return 0
	}
	switch v := extra[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// Analyze evaluates any ATIF trajectory against efficiency, tool health, loop, coding hygiene,
// terminal, context, and subagent heuristics.
func Analyze(traj *Trajectory) *Analysis {
	if traj == nil {
		return &Analysis{}
	}

	an := &Analysis{
		SessionID:  traj.SessionID,
		AgentName:  traj.Agent.Name,
		ModelName:  traj.Agent.ModelName,
		TotalSteps: len(traj.Steps),
		ToolHealth: ToolHealthStats{
			ToolCounts: make(map[string]int),
			CodeCounts: make(map[string]int),
		},
	}

	userTurns := 0
	agentSteps := 0
	llmCalls := 0
	promptTokens := 0
	compTokens := 0
	cachedTokens := 0
	reasoningTokens := 0
	totalCost := 0.0

	// Loop detection trackers
	type toolSig struct {
		name     string
		argsHash string
	}
	var lastReads = make(map[string]int)  // path -> last step read
	var lastWrites = make(map[string]int) // path -> last step written
	var consecutiveShellFails = make([]string, 0)
	var editFailuresPerPath = make(map[string]int)

	// Coding hygiene trackers
	var totalEdits int
	var totalWrites int
	var postWriteChecksCount int
	var lastWriteStep int

	// Terminal trackers
	var nonZeroExits int
	var totalShellCommands int

	// Context trackers
	var compEvents int
	var truncEvents int

	for _, step := range traj.Steps {
		switch step.Source {
		case StepSourceUser:
			userTurns++
		case StepSourceSystem:
			if strings.Contains(step.Message, "truncation") {
				truncEvents++
			}
			if strings.Contains(step.Message, "compaction") || strings.Contains(step.Message, "compression") {
				compEvents++
			}
		case StepSourceAgent:
			agentSteps++
			calls := 1
			if step.LLMCallCount != nil && *step.LLMCallCount > 0 {
				calls = *step.LLMCallCount
			}
			llmCalls += calls

			if step.Metrics != nil {
				if step.Metrics.PromptTokens != nil {
					promptTokens += *step.Metrics.PromptTokens
				}
				if step.Metrics.CompletionTokens != nil {
					compTokens += *step.Metrics.CompletionTokens
				}
				if step.Metrics.CachedTokens != nil {
					cachedTokens += *step.Metrics.CachedTokens
				}
				reasoningTokens += extraInt(step.Metrics.Extra, "reasoning_tokens")
				if step.Metrics.CostUSD != nil {
					totalCost += *step.Metrics.CostUSD
				}
			}

			// Tool loop detector within single step
			seenInStep := make(map[toolSig]int)

			for _, tc := range step.ToolCalls {
				an.ToolHealth.TotalCalls++
				an.ToolHealth.ToolCounts[tc.FunctionName]++

				argsJSON, err := json.Marshal(tc.Arguments)
				if err != nil {
					argsJSON = []byte("{}")
				}
				sig := toolSig{name: tc.FunctionName, argsHash: string(argsJSON)}
				seenInStep[sig]++

				switch tc.FunctionName {
				case "read_file":
					p, _ := tc.Arguments["file_path"].(string)
					if p == "" {
						p, _ = tc.Arguments["path"].(string)
					}
					if p != "" {
						if lastR, ok := lastReads[p]; ok {
							lastW := lastWrites[p]
							if lastW < lastR {
								an.Findings = append(an.Findings, Finding{
									Category: "loop",
									Severity: SeverityWarning,
									Title:    "Redundant file read without intervening modification",
									Detail:   fmt.Sprintf("File %q read in step %d and re-read in step %d with no writes in between.", p, lastR, step.StepID),
									Remedy:   "Rely on earlier read results or use working memory scratchpad rather than re-reading files.",
								})
							}
						}
						lastReads[p] = step.StepID
					}

				case "write_file":
					totalWrites++
					lastWriteStep = step.StepID
					p, _ := tc.Arguments["file_path"].(string)
					if p != "" {
						lastWrites[p] = step.StepID
					}

				case "edit":
					totalEdits++
					lastWriteStep = step.StepID
					p, _ := tc.Arguments["file_path"].(string)
					if p != "" {
						lastWrites[p] = step.StepID
					}

				case "run_project_checks":
					if lastWriteStep > 0 {
						postWriteChecksCount++
					}

				case "run_shell_command":
					totalShellCommands++
				}

				// Check observation results for this call
				if res, ok := observationFor(step, tc.ToolCallID); ok {
					status, _ := res.Extra["status"].(string)
					code, _ := res.Extra["code"].(string)
					if code != "" {
						an.ToolHealth.CodeCounts[code]++
					}

					if status == "error" || status == "denied" {
						an.ToolHealth.FailedCalls++
						if tc.FunctionName == "edit" {
							p, _ := tc.Arguments["file_path"].(string)
							editFailuresPerPath[p]++
						}
						if tc.FunctionName == "run_shell_command" {
							cmd, _ := tc.Arguments["command"].(string)
							if len(consecutiveShellFails) > 0 && consecutiveShellFails[len(consecutiveShellFails)-1] == cmd {
								an.Findings = append(an.Findings, Finding{
									Category: "loop",
									Severity: SeverityCritical,
									Title:    "Failed shell command retried unchanged",
									Detail:   fmt.Sprintf("Command %q failed repeatedly without modification.", cmd),
									Remedy:   "Fix command syntax, environment, or parameters before retrying.",
								})
							}
							consecutiveShellFails = append(consecutiveShellFails, cmd)
						}
					} else {
						an.ToolHealth.SuccessCalls++
					}

					if exitCode := extraInt(res.Extra, "exit_code"); exitCode != 0 {
						nonZeroExits++
					}
				}
			}

			for sig, count := range seenInStep {
				if count > 1 {
					an.Findings = append(an.Findings, Finding{
						Category: "loop",
						Severity: SeverityCritical,
						Title:    "Identical tool call repeated in same step",
						Detail:   fmt.Sprintf("Tool %q called %d times in step %d with identical arguments.", sig.name, count, step.StepID),
						Remedy:   "Deduplicate tool calls or check tool parameter generation in prompt.",
					})
				}
			}
		}
	}

	// Repeated edit failure analysis
	for p, count := range editFailuresPerPath {
		if count >= 2 {
			an.Findings = append(an.Findings, Finding{
				Category: "loop",
				Severity: SeverityCritical,
				Title:    "Repeated edit failures on single file",
				Detail:   fmt.Sprintf("Tool 'edit' failed %d times on %q.", count, p),
				Remedy:   "Ensure model re-reads fresh file content before edit or falls back to targeted write.",
			})
		}
	}

	// Coding hygiene check
	if (totalWrites > 0 || totalEdits > 0) && postWriteChecksCount == 0 {
		an.Findings = append(an.Findings, Finding{
			Category: "hygiene",
			Severity: SeverityWarning,
			Title:    "File modifications concluded without project checks",
			Detail:   fmt.Sprintf("%d file mutation(s) occurred, but run_project_checks was never executed.", totalWrites+totalEdits),
			Remedy:   "Verify changes after edits by running run_project_checks or unit tests.",
		})
	}

	// Ratio of edit vs write_file
	if totalWrites > 0 && totalEdits == 0 && (totalWrites > 3) {
		an.Findings = append(an.Findings, Finding{
			Category: "hygiene",
			Severity: SeverityInfo,
			Title:    "Heavy use of full file rewrites instead of edits",
			Detail:   fmt.Sprintf("%d write_file calls and 0 edit calls observed.", totalWrites),
			Remedy:   "Prefer 'edit' for targeted changes to preserve file context and conserve tokens.",
		})
	}

	// Terminal exit codes
	if totalShellCommands > 0 {
		exitFailureRate := float64(nonZeroExits) / float64(totalShellCommands)
		if exitFailureRate > 0.3 {
			an.Findings = append(an.Findings, Finding{
				Category: "terminal",
				Severity: SeverityWarning,
				Title:    "High rate of non-zero shell command exits",
				Detail:   fmt.Sprintf("%d of %d shell commands (%.1f%%) exited with non-zero status.", nonZeroExits, totalShellCommands, exitFailureRate*100),
				Remedy:   "Inspect command arguments, environment prerequisites, and flags.",
			})
		}
	}

	// Context and Compaction
	if truncEvents > 0 || compEvents > 0 {
		an.Findings = append(an.Findings, Finding{
			Category: "context",
			Severity: SeverityInfo,
			Title:    "Context compaction or truncation triggered",
			Detail:   fmt.Sprintf("%d compaction(s) and %d truncation(s) recorded during session.", compEvents, truncEvents),
			Remedy:   "Use subagent delegations to isolate research or optimize token overhead.",
		})
	}

	// Subagents
	if len(traj.SubagentTrajectories) > 0 {
		an.Findings = append(an.Findings, Finding{
			Category: "subagents",
			Severity: SeverityInfo,
			Title:    "Subagent delegations utilized",
			Detail:   fmt.Sprintf("%d child subagent trajectories recorded.", len(traj.SubagentTrajectories)),
			Remedy:   "Monitor subagent token share and failure rates.",
		})
	}

	// Calculate final efficiency stats
	an.Efficiency = EfficiencyStats{
		UserTurns:       userTurns,
		AgentSteps:      agentSteps,
		LLMCalls:        llmCalls,
		TotalTokens:     promptTokens + compTokens,
		PromptTokens:    promptTokens,
		CompTokens:      compTokens,
		CachedTokens:    cachedTokens,
		ReasoningTokens: reasoningTokens,
		TotalCostUSD:    totalCost,
	}
	if userTurns > 0 {
		an.Efficiency.StepsPerTurn = float64(agentSteps) / float64(userTurns)
	}
	if (promptTokens + cachedTokens) > 0 {
		an.Efficiency.CacheHitRatio = float64(cachedTokens) / float64(promptTokens+cachedTokens)
	}
	if an.ToolHealth.TotalCalls > 0 {
		an.ToolHealth.FailureRate = float64(an.ToolHealth.FailedCalls) / float64(an.ToolHealth.TotalCalls)
	}

	an.TotalTokens = an.Efficiency.TotalTokens
	an.TotalCost = totalCost

	// Sort findings by severity
	sort.SliceStable(an.Findings, func(i, j int) bool {
		order := map[FindingSeverity]int{
			SeverityCritical: 0,
			SeverityWarning:  1,
			SeverityInfo:     2,
		}
		return order[an.Findings[i].Severity] < order[an.Findings[j].Severity]
	})

	return an
}

// Compare produces a structured comparison delta between two analyses.
func Compare(a, b *Analysis) string {
	if a == nil || b == nil {
		return "Cannot compare: one or both analyses are nil."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "=== Trajectory Comparison: %s vs %s ===\n\n", a.SessionID, b.SessionID)
	fmt.Fprintf(&sb, "%-25s | %-15s | %-15s | %-15s\n", "Metric", a.SessionID, b.SessionID, "Delta")
	fmt.Fprintf(&sb, "%s\n", strings.Repeat("-", 75))

	printRow := func(label string, valA, valB float64, format string, higherIsBetter bool) {
		delta := valB - valA
		deltaStr := fmt.Sprintf(format, delta)
		if delta > 0 {
			deltaStr = "+" + deltaStr
		}
		fmt.Fprintf(&sb, "%-25s | %-15s | %-15s | %-15s\n",
			label,
			fmt.Sprintf(format, valA),
			fmt.Sprintf(format, valB),
			deltaStr,
		)
	}

	printRow("Total Steps", float64(a.TotalSteps), float64(b.TotalSteps), "%.0f", false)
	printRow("LLM Calls", float64(a.Efficiency.LLMCalls), float64(b.Efficiency.LLMCalls), "%.0f", false)
	printRow("Total Tokens", float64(a.TotalTokens), float64(b.TotalTokens), "%.0f", false)
	printRow("Prompt Tokens", float64(a.Efficiency.PromptTokens), float64(b.Efficiency.PromptTokens), "%.0f", false)
	printRow("Completion Tokens", float64(a.Efficiency.CompTokens), float64(b.Efficiency.CompTokens), "%.0f", false)
	printRow("Cached Tokens", float64(a.Efficiency.CachedTokens), float64(b.Efficiency.CachedTokens), "%.0f", true)
	printRow("Cache Hit Ratio", a.Efficiency.CacheHitRatio*100, b.Efficiency.CacheHitRatio*100, "%.1f%%", true)
	printRow("Total Cost ($)", a.TotalCost, b.TotalCost, "$%.4f", false)
	printRow("Tool Calls", float64(a.ToolHealth.TotalCalls), float64(b.ToolHealth.TotalCalls), "%.0f", false)
	printRow("Tool Failures", float64(a.ToolHealth.FailedCalls), float64(b.ToolHealth.FailedCalls), "%.0f", false)
	printRow("Tool Failure Rate", a.ToolHealth.FailureRate*100, b.ToolHealth.FailureRate*100, "%.1f%%", false)

	fmt.Fprintf(&sb, "\nFindings count: %s has %d findings, %s has %d findings.\n",
		a.SessionID, len(a.Findings), b.SessionID, len(b.Findings))

	return sb.String()
}

// RenderText formats an analysis as a human-readable text report.
func RenderText(an *Analysis) string {
	if an == nil {
		return "No analysis."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "=== ATIF Trajectory Analysis: %s ===\n", an.SessionID)
	if an.AgentName != "" {
		fmt.Fprintf(&sb, "Agent: %s", an.AgentName)
		if an.ModelName != "" {
			fmt.Fprintf(&sb, " (Model: %s)", an.ModelName)
		}
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, "Total Steps: %d | User Turns: %d | LLM Calls: %d\n", an.TotalSteps, an.Efficiency.UserTurns, an.Efficiency.LLMCalls)
	fmt.Fprintf(&sb, "Tokens: %d (Prompt: %d, Comp: %d, Cached: %d, Reasoning: %d)\n",
		an.TotalTokens, an.Efficiency.PromptTokens, an.Efficiency.CompTokens, an.Efficiency.CachedTokens, an.Efficiency.ReasoningTokens)
	if an.Efficiency.CacheHitRatio > 0 {
		fmt.Fprintf(&sb, "Cache Hit Ratio: %.1f%%\n", an.Efficiency.CacheHitRatio*100)
	}
	if an.TotalCost > 0 {
		fmt.Fprintf(&sb, "Total Cost: $%.4f\n", an.TotalCost)
	}
	fmt.Fprintf(&sb, "Tool Calls: %d (Success: %d, Failed: %d, Failure Rate: %.1f%%)\n",
		an.ToolHealth.TotalCalls, an.ToolHealth.SuccessCalls, an.ToolHealth.FailedCalls, an.ToolHealth.FailureRate*100)

	if len(an.Findings) > 0 {
		fmt.Fprintf(&sb, "\nFindings (%d):\n", len(an.Findings))
		for i, f := range an.Findings {
			fmt.Fprintf(&sb, " %d. [%s] [%s] %s\n    Detail: %s\n    Remedy: %s\n",
				i+1, strings.ToUpper(string(f.Severity)), f.Category, f.Title, f.Detail, f.Remedy)
		}
	} else {
		sb.WriteString("\nNo problematic findings detected. Execution was clean.\n")
	}

	return sb.String()
}
