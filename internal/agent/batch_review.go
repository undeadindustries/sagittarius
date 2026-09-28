package agent

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/prompt"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
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

// batchReviewTotalDiffRunes caps the combined diff text for one review. Past
// it the reviewer gets a file list to read itself rather than a truncated
// wall of diffs that hides the seams it exists to check.
const batchReviewTotalDiffRunes = 24000

// batchOutcome is one successful code_task child's contribution to the batch
// review: what it was asked to do, what it reported, and what it touched.
type batchOutcome struct {
	itemIndex int
	desc      string
	summary   string
	files     []string
	lease     []string
}

// runBatchReview is the scheduler's subagent batch finalizer (AD-153). It
// reviews all of a turn's successful code_task children at once — against the
// shared contract and, crucially, against each other, since the cross-file
// seams are exactly what no per-child review can see. On a FAIL it runs one
// bounded fix-and-re-review loop; a batch that still fails is handed to the
// parent as needs_changes with the findings attached.
func (r *Runner) runBatchReview(ctx context.Context, batch []tools.SubagentBatchItem, emit func(ui.StreamEvent)) error {
	if !config.ReviewSubagentsEnabled(r.settingsSnapshot(), nil) {
		return nil
	}

	contract := ""
	var outcomes []batchOutcome
	for i, item := range batch {
		if contract == "" {
			contract, _ = item.Call.Args[tools.CodeTaskParamContract].(string)
		}
		resp := item.Response.Response
		if resp == nil {
			continue
		}
		if errText, _ := resp["error"].(string); errText != "" {
			continue
		}
		// Only completed children are reviewed: a failed child changed nothing
		// the parent trusts, and an incomplete (round-capped) one is reported
		// as partial work already.
		if status, _ := resp["status"].(string); status != subagentStatusCompleted {
			continue
		}
		files := stringList(resp["files_changed"])
		if len(files) == 0 {
			continue
		}
		desc, _ := item.Call.Args[tools.TaskParamDescription].(string)
		summary, _ := resp["summary"].(string)
		outcomes = append(outcomes, batchOutcome{
			itemIndex: i,
			desc:      desc,
			summary:   summary,
			files:     files,
			lease:     stringList(resp["lease"]),
		})
	}
	if len(outcomes) == 0 {
		return nil
	}

	var files, leasePatterns []string
	seenFile, seenLease := map[string]bool{}, map[string]bool{}
	for _, o := range outcomes {
		for _, f := range o.files {
			if !seenFile[f] {
				seenFile[f] = true
				files = append(files, f)
			}
		}
		for _, p := range o.lease {
			if !seenLease[p] {
				seenLease[p] = true
				leasePatterns = append(leasePatterns, p)
			}
		}
	}

	baseID := batch[outcomes[0].itemIndex].Call.ID
	review := r.reviewAndFix(ctx, baseID, contract, outcomes, files, leasePatterns, emit)

	for k, o := range outcomes {
		resp := batch[o.itemIndex].Response.Response
		if k == 0 {
			resp["batch_review"] = review
			continue
		}
		resp["batch_review"] = fmt.Sprintf("verdict %s; the full batch review is on the first code_task result", review["verdict"])
	}
	return nil
}

// reviewAndFix runs the reviewer, then the bounded fix-and-re-review loop
// while the verdict is FAIL. The returned map is the batch_review block
// attached to the hand-off.
func (r *Runner) reviewAndFix(ctx context.Context, baseID, contract string, outcomes []batchOutcome, files, leasePatterns []string, emit func(ui.StreamEvent)) map[string]any {
	maxFix := config.ResolveSubagentMaxFixRounds(r.sagittariusSettings(), config.DefaultSubagentMaxFixRounds)

	review, err := r.reviewBatch(ctx, baseID, 1, contract, outcomes, files, emit)
	if err != nil {
		return map[string]any{"verdict": reviewVerdictError, "findings": err.Error()}
	}

	fixRounds := 0
	var fixFiles []string
	for review["verdict"] == reviewVerdictFail && fixRounds < maxFix {
		fixRounds++
		findings, _ := review["findings"].(string)
		fixed, fixErr := r.runBatchFix(ctx, baseID, fixRounds, contract, leasePatterns, findings, files, emit)
		if fixErr != nil {
			if ctx.Err() != nil {
				return map[string]any{"verdict": reviewVerdictError, "findings": fixErr.Error()}
			}
			// A fix child that cannot run leaves the failing review in place;
			// the parent gets needs_changes below.
			fixRounds--
			break
		}
		fixFiles = unionStrings(fixFiles, fixed)
		files = unionStrings(files, fixed)
		review, err = r.reviewBatch(ctx, baseID, fixRounds+1, contract, outcomes, files, emit)
		if err != nil {
			return map[string]any{"verdict": reviewVerdictError, "findings": err.Error()}
		}
	}

	out := map[string]any{
		"verdict":    review["verdict"],
		"findings":   review["findings"],
		"fix_rounds": fixRounds,
	}
	if len(fixFiles) > 0 {
		out["fix_files_changed"] = fixFiles
	}
	if review["verdict"] == reviewVerdictFail {
		out["status"] = "needs_changes"
		out["next_step"] = "The automatic fix round did not resolve the review findings. " +
			"Fix the listed findings yourself before finishing — do not re-delegate the same task."
	}
	return out
}

// reviewBatch launches one reviewer child over the whole batch and parses its
// verdict. Only cancellation is returned as an error; a reviewer that cannot
// start or fails mid-run is reported as verdict "error", never fatal to the
// hand-off.
func (r *Runner) reviewBatch(ctx context.Context, baseID string, round int, contract string, outcomes []batchOutcome, files []string, emit func(ui.StreamEvent)) (map[string]any, error) {
	cardID := fmt.Sprintf("%s#review-%d", baseID, round)
	emit(ui.StreamEvent{
		Type: ui.StreamToolStart, ToolName: "batch_review", ToolCallID: cardID,
		Text:  fmt.Sprintf("batch review, round %d (%d files)", round, len(files)),
		Badge: subagentBadge(r, config.SubagentReviewer),
	})
	finish := func(text string, isErr bool) {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "batch_review", ToolCallID: cardID, Text: text, IsError: isErr})
	}

	reviewPrompt := r.batchReviewPrompt(contract, outcomes, files)
	// A reviewer that returns no verdict gives the batch nothing: an empty
	// deliverable (a reasoning-only reply ends a child turn cleanly) or a
	// missing verdict line both land here. Retry once with an explicit nudge
	// before accepting "unknown" — the retry is cheap next to the batch it
	// guards, and a silent no-op review is the worst outcome.
	var text string
	for attempt := 1; attempt <= 2; attempt++ {
		reviewer, err := r.newSubagent(ctx, subagentSpec{
			description: fmt.Sprintf("Batch review (round %d)", round),
			mode:        modes.ModeAsk,
			class:       config.SubagentReviewer,
			charter:     prompt.ReviewSubagentCharter(),
			approval:    r.approval,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			finish("reviewer could not start: "+err.Error(), true)
			return map[string]any{"verdict": reviewVerdictError, "findings": "reviewer could not start: " + err.Error()}, nil
		}

		var runErr error
		text, runErr = reviewer.run(ctx, reviewPrompt, cardSink(emit, cardID))
		if runErr != nil {
			if ctx.Err() != nil {
				return nil, runErr
			}
			finish(runErr.Error(), true)
			return map[string]any{"verdict": reviewVerdictError, "findings": runErr.Error()}, nil
		}
		if parseReviewVerdict(text) != reviewVerdictUnknown {
			break
		}
		if attempt == 1 {
			reviewPrompt += "\n\nYour previous reply had no usable verdict. Report your findings and end with exactly one line: VERDICT: PASS or VERDICT: FAIL."
		}
	}
	verdict := parseReviewVerdict(text)
	finish(capRunes(text, 400), verdict == reviewVerdictFail)
	return map[string]any{"verdict": verdict, "findings": capReviewFindings(text)}, nil
}

// runBatchFix launches one coding child to resolve review findings across the
// whole batch's lease union. It returns the files the fix child changed.
func (r *Runner) runBatchFix(ctx context.Context, baseID string, round int, contract string, leasePatterns []string, findings string, files []string, emit func(ui.StreamEvent)) ([]string, error) {
	lease, err := tools.ParseWriteLease(leasePatterns)
	if err != nil {
		return nil, fmt.Errorf("batch fix lease: %w", err)
	}

	cardID := fmt.Sprintf("%s#fix-%d", baseID, round)
	emit(ui.StreamEvent{
		Type: ui.StreamToolStart, ToolName: "batch_fix", ToolCallID: cardID,
		Text:  fmt.Sprintf("fix review findings, round %d", round),
		Badge: subagentBadge(r, config.SubagentCoding),
	})
	finish := func(text string, isErr bool) {
		emit(ui.StreamEvent{Type: ui.StreamToolResult, ToolName: "batch_fix", ToolCallID: cardID, Text: text, IsError: isErr})
	}

	child, err := r.newSubagent(ctx, subagentSpec{
		description: fmt.Sprintf("Fix review findings (round %d)", round),
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		lease:       &lease,
		charter:     prompt.FixSubagentCharter(lease.Patterns, contract),
		snapshotter: r.snap,
		approval:    ApprovalYolo,
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		finish("fix subagent could not start: "+err.Error(), true)
		return nil, fmt.Errorf("fix subagent could not start: %w", err)
	}

	_, runErr := child.run(ctx, batchFixPrompt(contract, findings, files), cardSink(emit, cardID))
	if runErr != nil {
		if ctx.Err() != nil {
			return nil, runErr
		}
		finish(runErr.Error(), true)
		return nil, runErr
	}
	changed := child.runner.writtenPaths()
	finish(fmt.Sprintf("changed %d file(s)", len(changed)), false)
	return changed, nil
}

// batchReviewPrompt assembles what the reviewer sees: the binding contract,
// each sibling's own summary, the files the batch touched, and per-file diffs
// when snapshots are available. When snapshotting is off the reviewer still
// has the file list and reads current contents itself.
func (r *Runner) batchReviewPrompt(contract string, outcomes []batchOutcome, files []string) string {
	var b strings.Builder
	b.WriteString("Review this finished batch of changes. Sibling coding subagents made them in parallel, each seeing only its own files.\n\n")
	if strings.TrimSpace(contract) != "" {
		fmt.Fprintf(&b, "Shared design contract (binding on every sibling):\n%s\n\n", contract)
	}
	for _, o := range outcomes {
		if o.desc == "" && strings.TrimSpace(o.summary) == "" {
			continue
		}
		fmt.Fprintf(&b, "Subagent %q reports:\n%s\n\n", o.desc, o.summary)
	}
	b.WriteString("Files changed:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	if diffs := r.batchReviewDiffs(files); diffs != "" {
		fmt.Fprintf(&b, "\nDiffs:\n%s\n", diffs)
	} else {
		b.WriteString("\nNo diffs are attached (snapshotting is off). Read the listed files yourself.\n")
	}
	return b.String()
}

// batchFixPrompt tells the fix child exactly what to repair and nothing more.
func batchFixPrompt(contract, findings string, files []string) string {
	var b strings.Builder
	b.WriteString("A review of the batch found defects. Fix exactly these findings and nothing else.\n\n")
	fmt.Fprintf(&b, "Review findings:\n%s\n\n", findings)
	b.WriteString("Files in scope:\n")
	for _, f := range files {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	if strings.TrimSpace(contract) != "" {
		fmt.Fprintf(&b, "\nThe batch's shared design contract still applies:\n%s\n", contract)
	}
	return b.String()
}

// batchReviewDiffs renders per-file unified diffs from the session snapshot
// index, capped per file and in total. Past the total cap the reviewer gets
// the remaining file names to read itself.
func (r *Runner) batchReviewDiffs(files []string) string {
	if r.snap == nil {
		return ""
	}
	var b strings.Builder
	total := 0
	for i, f := range files {
		d, err := r.snap.Diff(f)
		if err != nil || strings.TrimSpace(d) == "" {
			continue
		}
		d = capRunes(d, reviewDiffRunes)
		n := utf8.RuneCountInString(d)
		if total+n > batchReviewTotalDiffRunes {
			fmt.Fprintf(&b, "(diffs omitted for size; read these files yourself: %s)\n",
				strings.Join(files[i:], ", "))
			break
		}
		b.WriteString(d + "\n")
		total += n
	}
	return strings.TrimRight(b.String(), "\n")
}

// cardSink adapts a UI emit function to the ToolOutputSink a child reports
// progress through, so the synthetic card shows live activity while running.
func cardSink(emit func(ui.StreamEvent), cardID string) tools.ToolOutputSink {
	return func(text string) {
		emit(ui.StreamEvent{Type: ui.StreamToolOutput, ToolCallID: cardID, Text: text})
	}
}

// stringList reads a []string out of a hand-off map value.
func stringList(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// unionStrings appends the items of b that a lacks, preserving order.
func unionStrings(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	out := a
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
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
