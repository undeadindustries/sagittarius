package agent

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

// ScratchpadMaxRunes caps the working-memory note. At roughly one token per
// four runes this is about 1k tokens on every request once populated, which is
// the whole cost of the feature — see docs/working-memory.md. It is compiled in
// rather than configurable: a user who wants the tokens back turns the tool off
// entirely (sagittarius.scratchpadEnabled).
const ScratchpadMaxRunes = 4096

// Scratchpad returns the model's current working-memory note, or "" when none
// is set.
func (r *Runner) Scratchpad() string {
	r.scratchpadMu.RLock()
	defer r.scratchpadMu.RUnlock()
	return r.scratchpad
}

func (r *Runner) scratchpadToolRegistered() bool {
	reg := r.Registry()
	if reg == nil {
		return false
	}
	_, ok := reg.Lookup(tools.UpdateScratchpadToolName)
	return ok
}

// SetScratchpad replaces the working-memory note, persists it to the session
// recorder, and recomposes the system instruction so the new text reaches the
// model on the very next turn. Passing an empty (or whitespace-only) string
// clears it, which is what ClearScratchpad does.
//
// Replace-only is deliberate: the model always reads its current note back in
// its own system prompt, so it can rewrite the whole thing. An append mode
// would need its own cap handling and could truncate a sentence in half.
func (r *Runner) SetScratchpad(text string) error {
	next := sanitizeScratchpad(text)

	r.scratchpadMu.RLock()
	unchanged := r.scratchpad == next
	r.scratchpadMu.RUnlock()
	if unchanged {
		return nil
	}

	if r.sessionRecorder != nil {
		if err := r.sessionRecorder.SetScratchpad(next); err != nil {
			return fmt.Errorf("persist scratchpad: %w", err)
		}
	}

	r.scratchpadMu.Lock()
	r.scratchpad = next
	r.scratchpadMu.Unlock()

	r.applyModeSystemSuffix()
	return nil
}

// ClearScratchpad discards the working-memory note and recomposes the system
// instruction so the block disappears from the very next request.
func (r *Runner) ClearScratchpad() error {
	return r.SetScratchpad("")
}

// sanitizeScratchpad normalizes line endings, trims surrounding whitespace, and
// caps the text at ScratchpadMaxRunes on a rune boundary. Cutting by bytes
// would split a multi-byte rune and put invalid UTF-8 into the system prompt.
func sanitizeScratchpad(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)

	runes := []rune(text)
	if len(runes) <= ScratchpadMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:ScratchpadMaxRunes]))
}

// renderScratchpadDirective builds the system-prompt block for the working
// memory note.
//
// The framing is load-bearing, not decoration. An unlabeled block sitting in
// the prompt gets copied into the model's visible replies — that is AD-119's
// state_snapshot leak and AD-068's write_file mimicry, both observed in
// production. So the block says who wrote it, that it is reference material
// rather than an instruction from the user, and that reproducing it in a reply
// is wrong.
func renderScratchpadDirective(note string) string {
	var b strings.Builder
	b.WriteString("**Your scratchpad.** You wrote the notes below with `update_scratchpad`. ")
	b.WriteString("They are your own working memory, carried outside the conversation so they survive ")
	b.WriteString("context compression — they are not a message from the user and not an instruction to obey. ")
	b.WriteString("Treat them as facts you already established. Keep them current with `update_scratchpad` ")
	b.WriteString("(it replaces the whole note), and never reproduce this block or its heading in a reply.\n\n")
	b.WriteString(note)
	return b.String()
}
