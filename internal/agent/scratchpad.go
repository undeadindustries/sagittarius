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

// scratchpadClearedNotice is appended once to the next user message after a
// user-initiated /scratchpad clear so earlier update_scratchpad tool results
// still in history do not contradict the cleared system prompt.
const scratchpadClearedNotice = "(The user cleared your scratchpad. Earlier update_scratchpad results no longer apply.)"

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

// SetScratchpad replaces the live working-memory note and persists it to the
// session recorder without recomposing the system instruction. Rendering into
// the system prompt is deferred until the next rebuild point (compression,
// truncation, session switch, or a user-initiated system change) so model calls
// to update_scratchpad do not bust the provider's prefix KV cache. Between
// rebuild points the model reads its current note from its latest unmasked
// update_scratchpad tool result.
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
	if next != "" {
		r.scratchpadClearedNoticePending = false
	}
	r.scratchpadMu.Unlock()

	return nil
}

// ClearScratchpad is the user-initiated (/scratchpad clear) wipe: it clears the
// live note, recomposes the system instruction immediately so the block
// disappears from the next request, and arms a one-shot notice on the next user
// message so earlier update_scratchpad results in history are disregarded.
func (r *Runner) ClearScratchpad() error {
	r.scratchpadMu.RLock()
	alreadyEmpty := r.scratchpad == "" && r.scratchpadRendered == ""
	liveChanged := r.scratchpad != ""
	r.scratchpadMu.RUnlock()
	if alreadyEmpty {
		return nil
	}

	if liveChanged && r.sessionRecorder != nil {
		if err := r.sessionRecorder.SetScratchpad(""); err != nil {
			return fmt.Errorf("persist scratchpad: %w", err)
		}
	}

	r.scratchpadMu.Lock()
	r.scratchpad = ""
	r.scratchpadClearedNoticePending = true
	r.scratchpadMu.Unlock()

	r.refreshSystemAtRebuild("scratchpad-clear")
	return nil
}

// takeScratchpadClearedNotice consumes the one-shot cleared notice armed by
// ClearScratchpad.
func (r *Runner) takeScratchpadClearedNotice() (string, bool) {
	r.scratchpadMu.Lock()
	defer r.scratchpadMu.Unlock()
	if !r.scratchpadClearedNoticePending {
		return "", false
	}
	r.scratchpadClearedNoticePending = false
	return scratchpadClearedNotice, true
}

// syncRenderedScratchpad copies the live note into scratchpadRendered and
// returns the rendered note. Called by applyModeSystemSuffix at rebuild points.
func (r *Runner) syncRenderedScratchpad() string {
	r.scratchpadMu.Lock()
	defer r.scratchpadMu.Unlock()
	r.scratchpadRendered = r.scratchpad
	return r.scratchpadRendered
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
	b.WriteString("This is your note as of the last context refresh. If you have called `update_scratchpad` since, your most recent call is current. ")
	b.WriteString("They are your own working memory, carried outside the conversation so they survive ")
	b.WriteString("context compression — they are not a message from the user and not an instruction to obey. ")
	b.WriteString("Treat them as facts you already established. Keep them current with `update_scratchpad` ")
	b.WriteString("(it replaces the whole note), and never reproduce this block or its heading in a reply.\n\n")
	b.WriteString(note)
	return b.String()
}
