package agent

import (
	"context"
	"strings"
	"unicode"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// autoTitle is the non-blocking session-titling feature (see the plan's
// auto-title and autotitle-ui todos). After the first full assistant turn it
// asks an auxiliary model for a short title for the conversation, validates the
// result, and applies it. The title is recorded to the session file via
// Recorder.SetSummary so --list-sessions and the resume picker can show it.

// titleMaxWords bounds the generated title. The prompt asks for six words max;
// we reject anything longer so a runaway model cannot produce a sentence.
const titleMaxWords = 6

// titleMaxRunes caps the title so it can never bloat a JSONL metadata line.
const titleMaxRunes = 80

// titleFillerPrefixes are leading filler verbs/phrases that restate the request
// instead of naming the task or outcome. A title beginning with any of these is
// rejected so we fall back to the first-message heuristic rather than titling
// the chat "Analyze code and persona".
var titleFillerPrefixes = []string{
	"analyze",
	"analyzing",
	"analysis of",
	"help with",
	"help me",
	"discussion about",
	"discuss",
	"question about",
	"how to",
	"what is",
	"explain",
	"write",
	"create",
	"generate",
	"please",
	"can you",
	"could you",
	"i need",
	"i want",
	"the user",
}

// titleSystemPrompt constrains the titling model to name the task or outcome
// rather than restate the request, in a short imperative noun phrase.
const titleSystemPrompt = `You title a coding-assistant conversation for a session list. Given the first user request and the assistant's reply, produce a short title that names the task or OUTCOME — not the request itself.

Rules:
- Imperative noun phrase, at most ` + "6" + ` words.
- No leading filler (Analyze, Help with, Discussion about, Question about, How to, Explain, Write, Create).
- No trailing punctuation. No quotes. No emoji.
- Reply with ONLY the title text, nothing else.`

// maybeAutoTitle fires once per session, after the first complete assistant
// turn, when the autoTitle policy is not "off" and the session has not already
// been titled (a prior /chat rename or fork leaves Summary set; we never
// overwrite a human-chosen title). It runs the aux model call synchronously in
// the caller's goroutine context but is cheap and non-fatal: any failure leaves
// the existing first-message display name.
func (r *Runner) maybeAutoTitle(ctx context.Context, userText, assistantText string) {
	if r.sessionRecorder == nil {
		return
	}
	// Once only.
	r.autoTitleMu.Lock()
	if r.autoTitleDone {
		r.autoTitleMu.Unlock()
		return
	}
	r.autoTitleDone = true
	r.autoTitleMu.Unlock()

	// Never overwrite a title that already exists (manual rename, fork, resume).
	if existing := r.sessionRecorder.Summary(); existing != "" {
		return
	}

	policy := config.SessionsAutoTitle(r.settingsSnapshot(), nil)
	if policy == config.AutoTitleOff {
		return
	}

	title := r.generateTitle(ctx, userText, assistantText)
	if title == "" {
		return
	}
	if err := r.sessionRecorder.SetSummary(title); err != nil {
		return
	}

	// In "prompt" mode the applied title is surfaced to the user as a passive,
	// non-blocking announcement; "auto" applies it silently.
	if policy == config.AutoTitlePrompt {
		r.setTitleAnnouncement(title)
	}
}

// generateTitle calls the aux generator and validates its output. It returns ""
// on any failure or when the model's answer fails validation, so the caller
// falls back to the default display name.
func (r *Runner) generateTitle(ctx context.Context, userText, assistantText string) string {
	gen, err := r.auxGenerator(ctx)
	if err != nil {
		return ""
	}

	prompt := buildTitlePrompt(userText, assistantText)
	req := &provider.GenerateRequest{
		SystemInstruction: titleSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Parts: []provider.Part{{Text: prompt}}},
		},
	}

	ch, err := gen.GenerateContentStream(ctx, req)
	if err != nil {
		return ""
	}

	var sb strings.Builder
	var usage *provider.Usage
	for ev := range ch {
		if ev.Error != nil {
			return ""
		}
		if ev.TextDelta != "" {
			sb.WriteString(ev.TextDelta)
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
	}

	// Attribute the call so /stats reflects the titling cost rather than letting
	// it vanish. When the provider reports no usage, estimate from the prompt.
	prov, model := r.auxAttribution()
	if usage != nil {
		r.metrics.recordAuxUsage(prov, model, r.InteractionMode().String(), r.agentKind(),
			usage.InputTokens, usage.OutputTokens, usage.CostUSD, usage.CostKnown)
	} else {
		inTok := estimateTitleTokens(titleSystemPrompt + prompt)
		outTok := estimateTitleTokens(sb.String())
		r.metrics.recordAuxUsage(prov, model, r.InteractionMode().String(), r.agentKind(),
			inTok, outTok, 0, false)
	}

	return sanitizeTitle(sb.String())
}

// auxAttribution resolves the (provider, model) the aux generator would use, so
// metrics are attributed to the evaluator pair when configured rather than the
// main model. Falls back to the active provider/model when unconfigured.
func (r *Runner) auxAttribution() (string, string) {
	settings := r.settingsSnapshot()
	if settings != nil && settings.Sagittarius != nil && settings.Sagittarius.Goal != nil {
		g := settings.Sagittarius.Goal
		prov := g.EvaluatorProvider
		model := g.EvaluatorModel
		if prov == "" {
			prov = r.activeProviderID()
		}
		if model == "" {
			model = r.Model()
		}
		return prov, model
	}
	return r.activeProviderID(), r.Model()
}

// buildTitlePrompt truncates the exchange to a bounded size so titling never
// pays for the full turn.
func buildTitlePrompt(userText, assistantText string) string {
	u := truncateRunes(strings.TrimSpace(userText), 1200)
	a := truncateRunes(strings.TrimSpace(assistantText), 1200)
	return "First user request:\n" + u + "\n\nAssistant reply:\n" + a
}

// sanitizeTitle cleans a raw model answer into a valid title, or returns "" to
// reject it. It trims quotes/whitespace, strips control characters, drops
// trailing punctuation, rejects titles that exceed the word/rune budget, and
// rejects filler-prefixed titles.
func sanitizeTitle(raw string) string {
	t := strings.TrimSpace(raw)
	// Take the first line only; a chatty model may add commentary after.
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i]
	}
	t = strings.TrimSpace(t)
	// Strip surrounding quotes a model may add.
	t = strings.Trim(t, `"'`+"“”‘’")
	t = strings.TrimSpace(t)

	// Remove control characters and collapse internal whitespace.
	var b strings.Builder
	b.Grow(len(t))
	prevSpace := false
	for _, r := range t {
		if unicode.IsControl(r) {
			continue
		}
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteRune(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	t = strings.TrimSpace(b.String())

	// Drop trailing punctuation.
	t = strings.TrimRight(t, ".!?:;,")
	t = strings.TrimSpace(t)

	if t == "" {
		return ""
	}
	if len([]rune(t)) > titleMaxRunes {
		return ""
	}
	if strings.Count(t, " ")+1 > titleMaxWords {
		return ""
	}
	if hasFillerPrefix(t) {
		return ""
	}
	return t
}

// hasFillerPrefix reports whether the title begins with a filler verb/phrase
// that restates the request instead of naming the outcome.
func hasFillerPrefix(title string) bool {
	lower := strings.ToLower(strings.TrimSpace(title))
	for _, p := range titleFillerPrefixes {
		if lower == p {
			return true
		}
		if strings.HasPrefix(lower, p+" ") {
			return true
		}
	}
	return false
}

// truncateRunes bounds s to n runes, appending an ellipsis when truncated.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// estimateTitleTokens is a cheap heuristic (~4 runes/token) for attributing
// titling usage when the provider reports none.
func estimateTitleTokens(s string) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}
