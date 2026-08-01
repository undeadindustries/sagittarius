package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
	"github.com/undeadindustries/sagittarius/internal/lsp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// runPostWriteChecks executes the AD-080 post-write diagnostic pipeline.
func (r *Runner) runPostWriteChecks(ctx context.Context, out chan<- ui.StreamEvent, emit func(ui.StreamEvent), paths []string) {
	emit(ui.StreamEvent{Type: ui.StreamInfo, Text: "Running post-write checks..."})
	
	// Track edit loop stats
	r.updateEditLoopStats(paths)

	timeout := time.Duration(r.autoCheckTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	
	var diagnoser diagnostics.Diagnoser
	if r.runtime != nil && r.runtime.LSPPool != nil {
		diagnoser = lsp.NewPoolDiagnoser(r.runtime.LSPPool, r.workDir)
	}

	opts := diagnostics.Options{
		Root:       r.workDir,
		Paths:      paths,
		ModuleWide: r.autoCheckModuleWide,
		Timeout:    timeout,
		LSP:        diagnoser,
	}

	report, err := diagnostics.Collect(ctx, opts)
	if err != nil {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Failed to run checks: %v", err)})
		return
	}

	// Format report for the model
	var modelFeedback []string
	var userFeedback []string

	for _, f := range report.Findings {
		msg := fmt.Sprintf("[%s] %s", f.Tool, f.Message)
		if f.Severity == diagnostics.SeverityError || f.Severity == diagnostics.SeverityWarning {
			modelFeedback = append(modelFeedback, msg)
		}
		if f.Severity == diagnostics.SeverityStyle || f.Severity == diagnostics.SeverityError || f.Severity == diagnostics.SeverityWarning {
			userFeedback = append(userFeedback, msg)
		}
	}

	for _, t := range report.MissingTools {
		userFeedback = append(userFeedback, fmt.Sprintf("Missing tool: %s. Install hint: %s", t.Name, t.InstallHint))
	}

	// If no model-actionable issues found
	if len(modelFeedback) == 0 {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: "Checks passed."})
		// We could still show style hints to the user, but we don't inject into history
		for _, msg := range userFeedback {
			emit(ui.StreamEvent{Type: ui.StreamInfo, Text: msg})
		}
		return
	}

	// Tell the user
	emit(ui.StreamEvent{Type: ui.StreamInfo, Text: fmt.Sprintf("Checks failed (%d issues). Feedback provided to agent.", len(modelFeedback))})
	for _, msg := range userFeedback {
		emit(ui.StreamEvent{Type: ui.StreamInfo, Text: msg})
	}

	// Check loop detection
	nudge := ""
	if r.editLoopThreshold > 0 {
		for _, p := range paths {
			if r.getEditCount(p) >= r.editLoopThreshold {
				nudge = fmt.Sprintf("\n\n[SYSTEM NUDGE] You have repeatedly failed to fix %q. Please STOP applying the same edit. Analyze the diagnostic carefully, rethink your approach, and try a different strategy.", p)
				break
			}
		}
	}

	// Build the synthetic message
	feedbackText := fmt.Sprintf("The following issues were found by automated checks after your edits:\n\n%s%s", strings.Join(modelFeedback, "\n"), nudge)

	r.historyMu.Lock()
	r.history = append(r.history, provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{
			{Text: feedbackText},
		},
	})
	r.historyMu.Unlock()
}

func (r *Runner) updateEditLoopStats(paths []string) {
	for _, p := range paths {
		r.editStats[p]++
	}
}

func (r *Runner) getEditCount(path string) int {
	return r.editStats[path]
}

// ResetEditStats is a helper to clear tracking if needed, e.g. on manual verification pass
func (r *Runner) ResetEditStats(path string) {
	delete(r.editStats, path)
}