package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/diagnostics"
	"github.com/undeadindustries/sagittarius/internal/lsp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// runPostWriteChecks executes the AD-080 post-write diagnostic pipeline: it
// collects lint/format/build/LSP findings for paths written this turn,
// surfaces them to the user, and — for error/warning findings a model can act
// on — injects a synthetic feedback message into history (and the session
// recorder) so the model sees it on the next round.
//
// It reads every sagittarius.verify.* behavior from the live settings
// snapshot rather than a value cached at construction, so a mid-session
// /settings change (autoCheckModuleWide, autoCheckTimeoutSeconds,
// repoLocalTools, editLoopThreshold) takes effect on the very next call.
//
// It reports whether model-actionable feedback was injected; callers should
// only spend an extra agent-loop round when it returns true.
func (r *Runner) runPostWriteChecks(ctx context.Context, emit func(ui.StreamEvent), paths []string) bool {
	emit(ui.StreamEvent{Type: ui.StreamInfo, Text: "Running post-write checks..."})

	settings := r.settingsSnapshot()
	timeoutSecs := config.VerifyAutoCheckTimeoutSeconds(settings, nil)
	if timeoutSecs <= 0 {
		timeoutSecs = 60
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	var diagnoser diagnostics.Diagnoser
	if r.runtime != nil && r.runtime.LSPPool != nil {
		diagnoser = lsp.NewPoolDiagnoser(r.runtime.LSPPool)
	}

	opts := diagnostics.Options{
		Root:             r.workDir,
		Paths:            paths,
		ModuleWide:       config.VerifyAutoCheckModuleWide(settings, nil),
		Timeout:          timeout,
		LSP:              diagnoser,
		RepoLocalPolicy:  diagnostics.RepoLocalPolicy(config.VerifyRepoLocalTools(settings, nil)),
		ApproveRepoLocal: r.approveRepoLocal(ctx, emit),
	}

	report, err := diagnostics.Collect(ctx, opts)
	if err != nil {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Failed to run checks: %v", err)})
		return false
	}

	modelFeedback, userFeedback := classifyFindings(report)

	if len(modelFeedback) == 0 {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: "Checks passed."})
		for _, msg := range userFeedback {
			emit(ui.StreamEvent{Type: ui.StreamInfo, Text: msg})
		}
		return false
	}

	emit(ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Checks failed (%d issues). Feedback provided to agent.", len(modelFeedback))})
	for _, msg := range userFeedback {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: msg})
	}

	nudge := r.editLoopNudge(paths, config.VerifyEditLoopThreshold(settings, nil))
	feedbackText := fmt.Sprintf("The following issues were found by automated checks after your edits:\n\n%s%s", strings.Join(modelFeedback, "\n"), nudge)

	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{
			{Text: feedbackText},
		},
	})
	r.historyMu.Unlock()

	if r.sessionRecorder != nil {
		r.sessionRecorder.RecordUserMessage(feedbackText)
	}

	return true
}

// classifyFindings splits a diagnostics.Report into modelFeedback (sent to
// the model on the next round) and userFeedback (shown in the UI). Only
// error/warning findings are model-actionable; style findings and missing-
// tool notices are informational for the user only, never sent to the model.
func classifyFindings(report diagnostics.Report) (modelFeedback, userFeedback []string) {
	for _, f := range report.Findings {
		msg := fmt.Sprintf("[%s] %s", f.Tool, f.Message)
		switch f.Severity {
		case diagnostics.SeverityError, diagnostics.SeverityWarning:
			modelFeedback = append(modelFeedback, msg)
			userFeedback = append(userFeedback, msg)
		case diagnostics.SeverityStyle:
			userFeedback = append(userFeedback, msg)
		}
	}

	for _, t := range report.MissingTools {
		userFeedback = append(userFeedback, fmt.Sprintf("Missing tool: %s. Install hint: %s", t.Name, t.InstallHint))
	}

	return modelFeedback, userFeedback
}

// approveRepoLocal returns a diagnostics.ApproveRepoLocal callback that
// prompts the user through the standard tool-confirmation UI, or nil in
// headless sessions so diagnostics denies repo-local tools without blocking
// on a prompt no one can answer. A "for this session" approval is memoized in
// repoLocalGrants (mirroring Scheduler.sessionGrants) so the user is asked at
// most once per tool per CLI session, not once per post-write check run.
//
// This does not persist the decision to settings.json across restarts —
// sagittarius.verify.repoLocalTools remains the durable "always allow/deny"
// switch for that; only the interactive "prompt" policy is memoized here.
func (r *Runner) approveRepoLocal(ctx context.Context, emit func(ui.StreamEvent)) func(tool, path string) bool {
	if !r.interactive {
		return nil
	}
	return func(toolName, path string) bool {
		r.repoLocalMu.Lock()
		if r.repoLocalGrants[toolName] {
			r.repoLocalMu.Unlock()
			return true
		}
		r.repoLocalMu.Unlock()

		replyCh := make(chan ui.ConfirmDecision, 1)
		emit(ui.StreamEvent{
			Type:     ui.StreamToolConfirm,
			ToolName: toolName,
			Text: fmt.Sprintf("Run repo-local tool %q for automated post-write checks? "+
				"It was found inside the workspace at %s rather than a system install, "+
				"so it may execute repository-controlled code.", toolName, path),
			ConfirmReply: replyCh,
		})

		select {
		case <-ctx.Done():
			return false
		case decision := <-replyCh:
			if decision == ui.ConfirmSession {
				r.repoLocalMu.Lock()
				r.repoLocalGrants[toolName] = true
				r.repoLocalMu.Unlock()
			}
			return decision == ui.ConfirmOnce || decision == ui.ConfirmSession
		}
	}
}

// editLoopNudge increments the failing-edit count for paths and returns a
// system nudge the first (and only the first) time any of them crosses
// threshold, steering a model stuck repeating the same failing edit toward a
// different approach instead of re-prompting on every subsequent failure.
// threshold <= 0 disables the detector entirely.
func (r *Runner) editLoopNudge(paths []string, threshold int) string {
	if threshold <= 0 {
		return ""
	}

	r.editStatsMu.Lock()
	defer r.editStatsMu.Unlock()

	nudge := ""
	for _, p := range paths {
		r.editStats[p]++
		if nudge == "" && r.editStats[p] >= threshold && !r.nudgedPaths[p] {
			r.nudgedPaths[p] = true
			nudge = fmt.Sprintf("\n\n[SYSTEM NUDGE] You have repeatedly failed to fix %q. "+
				"Please STOP applying the same edit. Analyze the diagnostic carefully, "+
				"rethink your approach, and try a different strategy.", p)
		}
	}
	return nudge
}

// resetEditLoopStats clears the repeated-edit loop detector's per-turn state.
// Called at the start of every RunTurn so a long session never accumulates
// stale counts across unrelated turns.
func (r *Runner) resetEditLoopStats() {
	r.editStatsMu.Lock()
	defer r.editStatsMu.Unlock()
	r.editStats = make(map[string]int)
	r.nudgedPaths = make(map[string]bool)
}
