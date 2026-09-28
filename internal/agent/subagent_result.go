package agent

import (
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Child-to-parent hand-off statuses. They are derived by the harness from the
// child's own transcript — never parsed out of model-written JSON.
const (
	subagentStatusCompleted  = "completed"
	subagentStatusFailed     = "failed"
	subagentStatusIncomplete = "incomplete"
	// subagentStatusCanceled marks a child the user stopped from the TUI
	// (AD-154). It is distinct from failed: the work may be fine as far as it
	// went, and the parent must not treat it as a defect to retry.
	subagentStatusCanceled = "canceled"
)

// buildSubagentResult assembles the structured hand-off every subagent tool
// returns. Every field comes from the child's runner state (history, file
// registry, resolved pair), so the parent can trust files_changed without
// trusting the child's self-report:
//
//	status       completed | failed | incomplete (hit max tool rounds)
//	summary      the child's final message (LastAssistantText)
//	files_changed workspace-relative files the child actually wrote
//	checks       {ran, ok} from the child's last run_project_checks response
//	tool_calls   number of tool calls the child made
//	provider     the child's resolved provider id
//	model        the child's resolved model
//	attempt      which attempt this was (see maxAttempts)
//	max_attempts the configured cap (0 = unlimited)
//	next_step    follow-up instruction for the parent, when there is one
//
// result and files_written are kept as aliases for one release so existing
// prompts and card formatters keep working.
func buildSubagentResult(child *subagent, class config.SubagentClass, runErr error, attempt, maxAttempts int) map[string]any {
	summary := ""
	filesChanged := []string{}
	checksRan := false
	checksOK := false
	toolCalls := 0
	childProvider := ""
	childModel := ""
	if child != nil && child.runner != nil {
		summary = child.runner.LastAssistantText()
		filesChanged = child.runner.writtenPaths()
		checksRan, checksOK = lastChecksOutcome(child.runner.History())
		toolCalls = countHistoryToolCalls(child.runner.History())
		childProvider = child.runner.ActiveProviderID()
		childModel = child.runner.Model()
	}

	status := subagentStatusCompleted
	resultErr := ""
	if runErr != nil {
		if child != nil && child.roundCapped {
			status = subagentStatusIncomplete
		} else {
			status = subagentStatusFailed
		}
		resultErr = runErr.Error()
		if summary == "" || summary == "(subagent finished without producing text)" {
			summary = resultErr
		}
	}

	result := map[string]any{
		"status":        status,
		"summary":       summary,
		"result":        summary,
		"files_changed": filesChanged,
		"files_written": filesChanged,
		"checks":        map[string]any{"ran": checksRan, "ok": checksOK},
		"tool_calls":    toolCalls,
		"provider":      childProvider,
		"model":         childModel,
		"attempt":       attempt,
		"max_attempts":  maxAttempts,
	}
	if resultErr != "" && status == subagentStatusFailed {
		// A failed hand-off keeps the "error" key so the card renderer flags
		// it (formatToolResult lets "error" win). An incomplete hand-off
		// omits it so the card renders the structured status, files, and
		// checks below instead of just the cap message.
		result["error"] = resultErr
	}
	if next := subagentNextStep(class, status, filesChanged, checksRan, checksOK); next != "" {
		result["next_step"] = next
	}
	return result
}

// canceledSubagentResult builds the hand-off for a child the user stopped
// (AD-154). Unlike a failed launch, a canceled child keeps its files_changed:
// writes that landed before the cancel are real, and dropping them from the
// hand-off would hide them from the parent.
func canceledSubagentResult(child *subagent, class config.SubagentClass, runErr error, attempt, maxAttempts int) map[string]any {
	result := buildSubagentResult(child, class, runErr, attempt, maxAttempts)
	result["status"] = subagentStatusCanceled
	delete(result, "error")
	if summary, _ := result["summary"].(string); summary == "" || (runErr != nil && summary == runErr.Error()) {
		result["summary"] = "Canceled by user."
		result["result"] = result["summary"]
	}
	result["next_step"] = "The user canceled this task; files_changed may be partial. " +
		"Do not re-delegate or repeat it without asking the user first."
	return result
}

// subagentNextStep tells the parent what to do after a coding child changed
// files. The child's shell is read-only, so verification is always the
// parent's job — whether the child ran checks or not.
func subagentNextStep(class config.SubagentClass, status string, filesChanged []string, checksRan, checksOK bool) string {
	if class != config.SubagentCoding || len(filesChanged) == 0 {
		return ""
	}
	if status == subagentStatusIncomplete {
		return "The subagent hit its tool-round cap with files changed: re-read files_changed and finish the task yourself."
	}
	if checksRan && checksOK {
		return "The subagent's checks passed, but its shell is read-only: run tests covering files_changed yourself before continuing."
	}
	return "Verify files_changed yourself: run tests covering them (the subagent's shell is read-only and cannot run tests)."
}

// lastChecksOutcome scans history for the most recent run_project_checks tool
// response and reports its all_ok flag. A missing or malformed response means
// the child never ran checks.
func lastChecksOutcome(history []provider.Message) (ran, ok bool) {
	for i := len(history) - 1; i >= 0; i-- {
		for _, part := range history[i].Parts {
			resp := part.FunctionResponse
			if resp == nil || resp.Name != tools.ProjectChecksToolName {
				continue
			}
			ran = true
			if v, present := resp.Response["all_ok"]; present {
				ok, _ = v.(bool)
			}
			return ran, ok
		}
	}
	return false, false
}

// countHistoryToolCalls counts model-issued tool calls across history. It
// measures how much work the child did, independent of what it claims.
func countHistoryToolCalls(history []provider.Message) int {
	n := 0
	for _, msg := range history {
		if msg.Role != provider.RoleModel {
			continue
		}
		for _, part := range msg.Parts {
			if part.FunctionCall != nil && strings.TrimSpace(part.FunctionCall.Name) != "" {
				n++
			}
		}
	}
	return n
}
