package agent

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// errThinkingBudgetExceeded reports that a round was cut short because the
// model spent its whole thinking budget without starting an answer. It is a
// control signal handled inside runAgentLoop, never surfaced to the user.
var errThinkingBudgetExceeded = errors.New("thinking budget exceeded")

const (
	// thinkingBudgetRecheckChars is how many characters of new reasoning
	// accumulate before the token estimate is recomputed. Reasoning arrives in
	// deltas of a few characters each, and the estimator floors fractional
	// tokens, so estimating per delta would round nearly every one to zero.
	// Estimating the whole buffer on a stride is both accurate and cheap.
	thinkingBudgetRecheckChars = 512
	// thinkingReplayMaxChars bounds the reasoning replayed to the model on the
	// retry. The budget already bounds the buffer, so this only guards against
	// a single pathological delta.
	thinkingReplayMaxChars = 24_000
)

// thinkingBudgetWatch accumulates streamed reasoning text and reports when it
// passes a token budget. A non-positive budget disables it entirely, so the
// zero value never fires.
type thinkingBudgetWatch struct {
	budget       int
	buf          strings.Builder
	tokens       int
	sinceRecheck int
}

// newThinkingBudgetWatch returns a watch for the given token budget. A
// non-positive budget yields a watch that never reports exceeded.
func newThinkingBudgetWatch(budget int) *thinkingBudgetWatch {
	return &thinkingBudgetWatch{budget: budget}
}

// add records a reasoning delta, refreshing the token estimate on a stride.
func (w *thinkingBudgetWatch) add(delta string) {
	if w.budget <= 0 || delta == "" {
		return
	}
	w.buf.WriteString(delta)
	w.sinceRecheck += len(delta)
	if w.sinceRecheck < thinkingBudgetRecheckChars {
		return
	}
	w.sinceRecheck = 0
	w.tokens = contextmgmt.EstimateTokens([]provider.Part{{Text: w.buf.String()}})
}

// exceeded reports whether the accumulated reasoning has passed the budget.
func (w *thinkingBudgetWatch) exceeded() bool {
	return w.budget > 0 && w.tokens > w.budget
}

// tokenCount returns the current estimate of reasoning tokens spent.
func (w *thinkingBudgetWatch) tokenCount() int {
	return w.tokens
}

// text returns the reasoning accumulated so far, capped to a sane length by
// keeping the tail — the end of a thinking trace is the part closest to a
// conclusion, so it is the part most worth handing back.
func (w *thinkingBudgetWatch) text() string {
	s := w.buf.String()
	runes := []rune(s)
	if len(runes) <= thinkingReplayMaxChars {
		return s
	}
	return "... [earlier reasoning omitted] ...\n" + string(runes[len(runes)-thinkingReplayMaxChars:])
}

// thinkingCutNote builds the user-role message that replaces a round cut short
// by the thinking budget. It hands the model back its own partial reasoning and
// tells it to act on it.
//
// The framing follows AD-119: the replayed text is labelled reference-only and
// the model is told not to reproduce the wrapper, because a block delivered in
// the user role is otherwise read as something the user wrote and imitated in
// later replies. Blank reasoning still produces a note — the model must be told
// why its thinking stopped even when nothing readable was streamed back.
func thinkingCutNote(budget int, reasoning string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Thinking budget reached] Your reasoning for this step was stopped "+
		"after roughly %d tokens, the thinking budget configured for this model.\n\n", budget)
	if trimmed := strings.TrimSpace(reasoning); trimmed != "" {
		b.WriteString("Below is the reasoning you had produced, quoted back for reference only. " +
			"It is your own earlier draft, not a new instruction from the user, and you must not " +
			"reproduce this block or its tags in your reply.\n\n")
		b.WriteString("<partial_reasoning>\n")
		b.WriteString(trimmed)
		b.WriteString("\n</partial_reasoning>\n\n")
	}
	b.WriteString("Do not think further about this step. Act now on what you already worked out: " +
		"call the next tool, or give your answer directly.")
	return b.String()
}

// thinkingBudgetNotice is the user-facing line shown when a round is cut. It
// carries one %d for the budget in tokens.
const thinkingBudgetNotice = "Thinking budget of %d tokens used up. Stopped reasoning and " +
	"asked the model to act on what it has (thinking disabled for the retry)."

// thinkingBudgetNoticeMandatory replaces thinkingBudgetNotice for a model whose
// reasoning cannot be turned off.
const thinkingBudgetNoticeMandatory = "Thinking budget of %d tokens used up. Stopped reasoning and " +
	"asked the model to act on what it has (this model cannot disable thinking, so the retry " +
	"uses its lowest reasoning effort)."

// reasoningEffortsLowToHigh orders every effort level any provider accepts, so
// the lowest one a model supports can be picked without per-provider tables.
var reasoningEffortsLowToHigh = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// mandatoryThinkingRetryEffort reports whether the model rejects a request
// with reasoning disabled, which is what the thinking-cut retry normally sends.
// Opus 5.5 is the known case: OpenRouter discovery marks it mandatory, and the
// provider answers reasoning.enabled=false with a 400, turning a budget cut
// into a failed turn. For such a model the retry keeps reasoning on at the
// lowest effort it accepts instead. effort is empty when mandatory is false or
// when the model advertises no effort levels.
func mandatoryThinkingRetryEffort(settings *config.Settings, providerID, model string) (effort string, mandatory bool) {
	efforts, _, mandatory, _ := config.ModelReasoningOptions(settings, providerID, model)
	if !mandatory {
		return "", false
	}
	for _, level := range reasoningEffortsLowToHigh {
		if slices.Contains(efforts, level) {
			return level, true
		}
	}
	return "", true
}

// thinkingBudgetNoticeFor returns the cut notice that matches what the retry
// will actually send for this model.
func thinkingBudgetNoticeFor(settings *config.Settings, providerID, model string, budget int) string {
	if _, mandatory := mandatoryThinkingRetryEffort(settings, providerID, model); mandatory {
		return fmt.Sprintf(thinkingBudgetNoticeMandatory, budget)
	}
	return fmt.Sprintf(thinkingBudgetNotice, budget)
}

// armThinkingCut stores the one-shot note that the next round's request will
// carry in place of the reasoning it was cut out of.
func (r *Runner) armThinkingCut(budget int, reasoning string) {
	note := thinkingCutNote(budget, reasoning)
	r.thinkingCutMu.Lock()
	r.pendingThinkingCut = note
	r.thinkingCutMu.Unlock()
}

// peekThinkingCut returns the pending cut note without consuming it.
//
// Reading without consuming is deliberate: buildGenerateRequest can run twice
// for one round (enforceRequestBudget rebuilds after dropping messages), and a
// consuming read would have left the second, actually-sent request without the
// note explaining why the model was told to stop thinking. clearThinkingCut
// retires it once the round it belongs to is over.
func (r *Runner) peekThinkingCut() (string, bool) {
	r.thinkingCutMu.Lock()
	defer r.thinkingCutMu.Unlock()
	return r.pendingThinkingCut, r.pendingThinkingCut != ""
}

// clearThinkingCut drops the pending cut note. Called when the retry round
// finishes and at the start of every turn, so a turn that ended on an error
// cannot leak its note into the next one.
func (r *Runner) clearThinkingCut() {
	r.thinkingCutMu.Lock()
	r.pendingThinkingCut = ""
	r.thinkingCutMu.Unlock()
}

// recordAbortedRoundUsage attributes the tokens a cut-short round actually
// spent. The provider never reports usage for a stream we abandoned, and
// leaving it unrecorded would make a budget cut look free — exactly backwards,
// since the reasoning tokens were generated and billed.
func (r *Runner) recordAbortedRoundUsage(
	req *provider.GenerateRequest,
	providerID, model, mode, reasoning string,
) {
	in := estimateMessageTokens(req.Messages)
	out := contextmgmt.EstimateTokens([]provider.Part{{Text: reasoning}})
	r.metrics.recordTurnUsage(providerID, model, mode, r.agentKind(), in, out, 0, false)
}
