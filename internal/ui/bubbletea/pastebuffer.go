package bubbletea

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	largePasteLineThreshold = 5
	largePasteRuneThreshold = 500
)

var pastePlaceholderRe = regexp.MustCompile(`\[Pasted Text: \d+ (?:lines|chars)(?: #\d+)?\]`)

type expandedPaste struct {
	id string
}

type pasteStore struct {
	content  map[string]string
	expanded *expandedPaste
}

func newPasteStore() pasteStore {
	return pasteStore{
		content: make(map[string]string),
	}
}

// isLargePaste reports whether the text exceeds the thresholds for being
// considered a large paste. Note that exactly 5 lines is not collapsed.
func isLargePaste(s string) bool {
	lines := strings.Split(s, "\n")
	return len(lines) > largePasteLineThreshold || len([]rune(s)) > largePasteRuneThreshold
}

// capture normalises line endings, stores the pasted content, and returns the
// placeholder string to display. If the paste is not large, it returns the
// original string.
func (p *pasteStore) capture(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	if !isLargePaste(s) {
		return s
	}

	if p.content == nil {
		p.content = make(map[string]string)
	}

	lines := strings.Split(s, "\n")
	var base string
	if len(lines) > largePasteLineThreshold {
		base = fmt.Sprintf("[Pasted Text: %d lines]", len(lines))
	} else {
		base = fmt.Sprintf("[Pasted Text: %d chars]", len([]rune(s)))
	}

	id := base
	suffix := 2
	for {
		if _, exists := p.content[id]; !exists {
			break
		}
		id = strings.Replace(base, "]", fmt.Sprintf(" #%d]", suffix), 1)
		suffix++
	}

	p.content[id] = s
	return id
}

// expand replaces all known placeholders in s with their full text. Unknown
// placeholders are left unchanged.
func (p *pasteStore) expand(s string) string {
	if len(p.content) == 0 {
		return s
	}
	return pastePlaceholderRe.ReplaceAllStringFunc(s, func(match string) string {
		if text, ok := p.content[match]; ok {
			return text
		}
		return match
	})
}

// prune drops any stored content whose placeholder no longer appears in s,
// and drops the expanded mark if the expanded text no longer appears verbatim.
// It returns true if any placeholder or expanded state was detached.
func (p *pasteStore) prune(s string) bool {
	if len(p.content) == 0 && p.expanded == nil {
		return false
	}

	changed := false

	// If a paste is currently expanded, we check if its exact content still
	// exists in the input. If not, the user edited the expanded text, so it
	// becomes normal text and can no longer be collapsed.
	if p.expanded != nil {
		if content, ok := p.content[p.expanded.id]; !ok || !strings.Contains(s, content) {
			if ok {
				delete(p.content, p.expanded.id)
			}
			p.expanded = nil
			changed = true
		}
	}

	if len(p.content) == 0 {
		return changed
	}

	// Find all placeholders still present in s
	present := make(map[string]bool)
	matches := pastePlaceholderRe.FindAllString(s, -1)
	for _, m := range matches {
		present[m] = true
	}

	// Delete any that are missing (and not currently expanded)
	for id := range p.content {
		if !present[id] && (p.expanded == nil || p.expanded.id != id) {
			delete(p.content, id)
			changed = true
		}
	}

	return changed
}

// placeholderAt returns the placeholder string that the byte offset sits inside
// or immediately adjacent to, and its start/end byte offsets in s.
func placeholderAt(s string, offset int) (id string, start, end int, found bool) {
	matches := pastePlaceholderRe.FindAllStringIndex(s, -1)
	for _, loc := range matches {
		if offset >= loc[0] && offset <= loc[1] {
			return s[loc[0]:loc[1]], loc[0], loc[1], true
		}
	}
	return "", 0, 0, false
}
