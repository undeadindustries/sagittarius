package bubbletea

import "strings"

// inputHistory is a prompt-history navigator modeled on gemini-cli's
// useInputHistory hook. messages holds submitted prompts in chronological order
// (oldest first); index is -1 while composing and 0..len-1 while browsing, where
// 0 is the most recent prompt. Per-level edits are cached so returning to a level
// (including the in-progress draft at level -1) restores what was there.
type inputHistory struct {
	messages []string
	index    int
	cache    map[int]string
}

func newInputHistory() *inputHistory {
	return &inputHistory{index: -1, cache: map[int]string{}}
}

// record appends a submitted prompt and resets navigation state. Consecutive
// duplicates and blank lines are ignored so the history stays useful.
func (h *inputHistory) record(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		h.reset()
		return
	}
	if n := len(h.messages); n == 0 || h.messages[n-1] != msg {
		h.messages = append(h.messages, msg)
	}
	h.reset()
}

// reset returns to the composing level and drops the per-level edit cache.
func (h *inputHistory) reset() {
	h.index = -1
	h.cache = map[int]string{}
}

// entry returns the text for a navigation level: the cached edit if one exists,
// otherwise the original prompt (or "" for the compose level).
func (h *inputHistory) entry(idx int) string {
	if t, ok := h.cache[idx]; ok {
		return t
	}
	if idx < 0 || idx >= len(h.messages) {
		return ""
	}
	return h.messages[len(h.messages)-1-idx]
}

// up moves one step toward older prompts, caching the current text first. It
// returns the text to display and false when already at the oldest prompt.
func (h *inputHistory) up(current string) (string, bool) {
	if len(h.messages) == 0 || h.index >= len(h.messages)-1 {
		return "", false
	}
	h.cache[h.index] = current
	h.index++
	return h.entry(h.index), true
}

// down moves one step toward newer prompts (and finally the draft), caching the
// current text first. It returns false when not currently browsing history.
func (h *inputHistory) down(current string) (string, bool) {
	if h.index == -1 {
		return "", false
	}
	h.cache[h.index] = current
	h.index--
	return h.entry(h.index), true
}
