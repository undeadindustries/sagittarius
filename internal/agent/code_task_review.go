package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/prompt"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Review verdicts a reviewer child can return. "unknown" means the child never
// wrote a parseable verdict line; the hand-off still carries its findings.
const (
	reviewVerdictPass    = "pass"
	reviewVerdictFail    = "fail"
	reviewVerdictUnknown = "unknown"
	reviewVerdictError   = "error"
)

// reviewFindingsRunes caps the reviewer text carried to the parent. A review
// is advisory — it must never cost more context than the change it covers.
const reviewFindingsRunes = 4000

// reviewDiffRunes caps the per-file diff handed to the reviewer.
const reviewDiffRunes = 6000

// maybeReview runs the opt-in reviewer pass after a code_task child changed
// files. It returns nil when the reviewer is disabled or there is nothing to
// review. A reviewer failure is reported in the map (verdict "error"), never
// fatal to the hand-off; only context cancellation propagates as an error.
// The reviewer never counts toward the delegation attempt budget.
func (t *codeTaskTool) maybeReview(ctx context.Context, desc string, child *subagent, filesChanged []string, sink tools.ToolOutputSink) (map[string]any, error) {
	if !config.ReviewSubagentsEnabled(t.runner.settingsSnapshot(), nil) {
		return nil, nil
	}
	if len(filesChanged) == 0 {
		return nil, nil
	}

	review, err := t.runner.newSubagent(ctx, subagentSpec{
		description: "Review: " + desc,
		mode:        modes.ModeAsk,
		class:       config.SubagentReviewer,
		charter:     prompt.ReviewSubagentCharter(),
		approval:    t.runner.approval,
	})
	if err != nil {
		return nil, err
	}

	text, runErr := review.run(ctx, t.reviewPrompt(desc, child, filesChanged), sink)
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, runErr
		}
		return map[string]any{"verdict": reviewVerdictError, "findings": runErr.Error()}, nil
	}
	return map[string]any{
		"verdict":  parseReviewVerdict(text),
		"findings": capReviewFindings(text),
	}, nil
}

// reviewPrompt assembles what the reviewer sees: the change brief, the
// sibling's own summary, the files it touched, and per-file diffs when
// snapshots are available. When snapshotting is off the reviewer still has
// the file list and reads current contents itself.
func (t *codeTaskTool) reviewPrompt(desc string, child *subagent, filesChanged []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review this finished change: %s\n\n", desc)
	if summary := child.runner.LastAssistantText(); strings.TrimSpace(summary) != "" {
		fmt.Fprintf(&b, "The authoring subagent reports:\n%s\n\n", summary)
	}
	fmt.Fprintf(&b, "Files changed:\n")
	for _, f := range filesChanged {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	if diffs := t.reviewDiffs(filesChanged); diffs != "" {
		fmt.Fprintf(&b, "\nDiffs:\n%s\n", diffs)
	} else {
		b.WriteString("\nNo diffs are attached (snapshotting is off). Read the listed files yourself.\n")
	}
	return b.String()
}

// reviewDiffs renders per-file unified diffs for the reviewer's files from the
// session snapshot index, capped per file. A file with no snapshot record
// contributes nothing — the reviewer reads it live instead.
func (t *codeTaskTool) reviewDiffs(filesChanged []string) string {
	snap := t.runner.snap
	if snap == nil {
		return ""
	}
	var b strings.Builder
	for _, f := range filesChanged {
		d, err := snap.Diff(f)
		if err != nil || strings.TrimSpace(d) == "" {
			continue
		}
		b.WriteString(capRunes(d, reviewDiffRunes) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseReviewVerdict reads the reviewer's closing verdict line. Matching is
// case-insensitive and accepts PASS/FAIL with common synonyms; anything else
// (including a missing line) is "unknown" rather than an error.
func parseReviewVerdict(text string) string {
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "VERDICT:") {
			// Only the trailing verdict line counts; findings above it may
			// quote the word verdict without meaning it.
			return reviewVerdictUnknown
		}
		rest := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(upper, "VERDICT:")))
		switch {
		case strings.HasPrefix(rest, "PASS") || strings.HasPrefix(rest, "APPROVE") || strings.HasPrefix(rest, "LGTM"):
			return reviewVerdictPass
		case strings.HasPrefix(rest, "FAIL") || strings.HasPrefix(rest, "REJECT") || strings.HasPrefix(rest, "REQUEST CHANGES"):
			return reviewVerdictFail
		default:
			return reviewVerdictUnknown
		}
	}
	return reviewVerdictUnknown
}

// capReviewFindings bounds the reviewer text with the shared rune cutter.
func capReviewFindings(text string) string {
	return capRunes(text, reviewFindingsRunes)
}
