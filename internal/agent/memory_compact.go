package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/diff"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// A compaction may not drop more than half the entries. Past that point the
// result is treated as a model failure rather than an aggressive merge:
// /memory compact must never be the reason a user's recorded facts vanish.
const (
	memoryCompactMinKeptNumer = 1
	memoryCompactMinKeptDenom = 2
)

const memoryCompactSystemPrompt = `You are compacting a list of facts a coding agent recorded across past sessions. Each input line has the form "- (YYYY-MM-DD) fact".

Return the revised list in exactly that form, one fact per line, and nothing else: no preamble, no commentary, no headings, no code fences.

Rules:
- Merge duplicates and near-duplicates into a single line.
- Combine facts about the same subject when combining them loses no detail.
- Drop a fact only when a later fact plainly supersedes it.
- Keep every fact that still stands on its own. When in doubt, keep it.
- Preserve each fact's date. When you merge several facts, use the newest of their dates.
- Do not invent facts, dates, or detail that is not in the input.`

// compactProposal is a previewed MEMORY.md rewrite awaiting confirmation.
// Nothing is written until ApplyMemoryCompact consumes it.
type compactProposal struct {
	Path     string
	Scope    config.SettingScope
	Rendered string
	Before   int
	After    int
}

// PreviewMemoryCompact asks a model to merge scope's MEMORY.md and stores the
// result for ApplyMemoryCompact. It writes nothing: compaction deletes
// user-owned content, so it does not get to be fire-and-forget.
func (r *Runner) PreviewMemoryCompact(ctx context.Context, scope config.SettingScope) (string, error) {
	path, err := writeMemoryPath(scope, r.WorkDir())
	if err != nil {
		return "", err
	}
	original, err := readFileIfExists(path)
	if err != nil {
		return "", err
	}
	before := parseMemoryOnlyFile(splitLines(original))
	if len(before) == 0 {
		return "", fmt.Errorf("%s has no memory entries to compact", path)
	}
	if len(before) < 2 {
		return "", fmt.Errorf("%s has one entry; nothing to merge", path)
	}

	raw, err := r.generateCompact(ctx, before)
	if err != nil {
		return "", err
	}
	after, err := parseCompactModelOutput(raw, before)
	if err != nil {
		return "", err
	}

	rendered := renderMemoryOnlyFile(after)
	if rendered == original {
		return "", fmt.Errorf("%s is already compact; nothing to change", path)
	}

	proposal := &compactProposal{
		Path:     path,
		Scope:    scope,
		Rendered: rendered,
		Before:   len(before),
		After:    len(after),
	}
	r.compactMu.Lock()
	r.pendingCompact = proposal
	r.compactMu.Unlock()

	return formatCompactPreview(proposal, original, r.compactModelLabel()), nil
}

// ApplyMemoryCompact writes the stored proposal and reloads the system
// instruction. The proposal is consumed whether or not the write succeeds, so
// a failed apply cannot be retried against stale content.
func (r *Runner) ApplyMemoryCompact(ctx context.Context) (string, error) {
	_ = ctx
	r.compactMu.Lock()
	proposal := r.pendingCompact
	r.pendingCompact = nil
	r.compactMu.Unlock()

	if proposal == nil {
		return "", fmt.Errorf("no compaction is pending; run /memory compact first")
	}
	if err := writeFileAtomic(proposal.Path, proposal.Rendered); err != nil {
		return "", err
	}
	if err := r.ReloadSystemInstruction(); err != nil {
		return "", fmt.Errorf("compacted %s, but reload failed: %w", proposal.Path, err)
	}
	return proposal.Path, nil
}

// AbortMemoryCompact discards a stored proposal.
func (r *Runner) AbortMemoryCompact() error {
	r.compactMu.Lock()
	pending := r.pendingCompact != nil
	r.pendingCompact = nil
	r.compactMu.Unlock()

	if !pending {
		return fmt.Errorf("no compaction is pending")
	}
	return nil
}

// compactModelLabel names the model that produced a proposal, and says
// plainly when it is the primary model rather than a dedicated evaluator.
// One deliberate user-invoked call on the primary model is acceptable here,
// unlike AD-097's per-turn classifier which silently double-billed.
func (r *Runner) compactModelLabel() string {
	pair := r.evaluatorPairLabel()
	if r.hasDedicatedEvaluator() {
		return pair
	}
	return pair + " (primary model; set sagittarius.goal.evaluatorModel to use a cheaper one)"
}

func (r *Runner) generateCompact(ctx context.Context, before []memoryLine) (string, error) {
	gen, err := r.auxGenerator(ctx)
	if err != nil {
		return "", fmt.Errorf("compact memory: %w", err)
	}

	prompt := renderMemoryOnlyFile(before)
	req := &provider.GenerateRequest{
		SystemInstruction: memoryCompactSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Parts: []provider.Part{{Text: prompt}}},
		},
	}

	ch, err := gen.GenerateContentStream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("compact memory: %w", err)
	}

	var sb strings.Builder
	var usage *provider.Usage
	for ev := range ch {
		if ev.Error != nil {
			return "", fmt.Errorf("compact memory: %w", ev.Error)
		}
		if ev.TextDelta != "" {
			sb.WriteString(ev.TextDelta)
		}
		if ev.Usage != nil {
			usage = ev.Usage
		}
	}

	// Attribute the call so /stats reflects the compaction cost instead of
	// letting it vanish.
	prov, model := r.auxAttribution()
	mode := r.InteractionMode().String()
	if usage != nil {
		r.metrics.recordAuxUsage(prov, model, mode, r.agentKind(),
			usage.InputTokens, usage.OutputTokens, usage.CostUSD, usage.CostKnown)
	} else {
		r.metrics.recordAuxUsage(prov, model, mode, r.agentKind(),
			estimateTitleTokens(memoryCompactSystemPrompt+prompt), estimateTitleTokens(sb.String()), 0, false)
	}

	return sb.String(), nil
}

// parseCompactModelOutput turns a model reply into entries, treating the reply
// as untrusted: every line is sanitized like an ordinary add, a date the input
// never contained is replaced rather than honored, and an empty or
// over-aggressive result is an error so the file is left untouched.
func parseCompactModelOutput(raw string, before []memoryLine) ([]memoryLine, error) {
	inputDates := make(map[string]struct{}, len(before))
	var newest time.Time
	for _, e := range before {
		if e.Date.IsZero() {
			continue
		}
		inputDates[e.Date.Format(memoryDateLayout)] = struct{}{}
		if e.Date.After(newest) {
			newest = e.Date
		}
	}

	var out []memoryLine
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || !compactLineIsEntry(trimmed) {
			continue
		}
		parsed := parseMemoryBullet(stripBulletPrefix(trimmed))
		text := sanitizeMemoryText(parsed.Text)
		if text == "" {
			continue
		}
		out = append(out, memoryLine{Date: compactEntryDate(parsed.Date, inputDates, newest), Text: text})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("compaction produced no entries; %s left unchanged", config.MemoryFileName)
	}
	if len(out)*memoryCompactMinKeptDenom < len(before)*memoryCompactMinKeptNumer {
		return nil, fmt.Errorf("compaction dropped too much (%d entries in, %d out); %s left unchanged",
			len(before), len(out), config.MemoryFileName)
	}
	return out, nil
}

// compactLineIsEntry requires a model's line to carry a bullet marker or a
// leading date. A stored file is parsed more leniently so a hand-written
// bullet still counts, but the prompt asks the model for an exact shape, and
// holding it to that is what stops a refusal sentence ("I can't help with
// that") from being written back as if it were a recorded fact.
func compactLineIsEntry(trimmed string) bool {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return memoryDatePrefix.MatchString(trimmed)
}

// compactEntryDate keeps a date the input actually contained and otherwise
// falls back to the newest input date, which is the merge rule and also stops
// a model from backdating or postdating an entry.
func compactEntryDate(date time.Time, inputDates map[string]struct{}, newest time.Time) time.Time {
	if date.IsZero() {
		return newest
	}
	if _, ok := inputDates[date.Format(memoryDateLayout)]; ok {
		return date
	}
	return newest
}

func formatCompactPreview(p *compactProposal, original, modelLabel string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Proposed compaction of %s\n", p.Path)
	fmt.Fprintf(&sb, "Model: %s\n", modelLabel)
	fmt.Fprintf(&sb, "Entries: %d → %d\n\n", p.Before, p.After)
	sb.WriteString(diff.UnifiedDiff(original, p.Rendered, p.Path))
	sb.WriteString("\nNothing has been written. Run \"/memory compact apply\" to accept, or \"/memory compact abort\" to discard.")
	return sb.String()
}
