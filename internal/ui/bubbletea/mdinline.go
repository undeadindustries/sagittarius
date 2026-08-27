package bubbletea

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Inline markdown scanning for one already-wrapped line of prose.
//
// Emphasis follows CommonMark's left/right-flanking delimiter rules rather than
// a naive `_(.+?)_` pattern, so identifiers a coding agent emits constantly —
// apply_xp_and_level_up, *args, uint8_t* — survive verbatim instead of having
// their delimiters eaten. Go's RE2 has no lookaround, so this is a hand-written
// scanner (the same reason internal/atmention parses by hand).
//
// Two deliberate departures from the spec:
//
//   - Python dunders are kept literal. Strict CommonMark bolds __init__ and
//     __name__ because both are whitespace-flanked, which is a real defect for
//     source text. The cost is that single-word __bold__ renders literally;
//     multi-word __bold text__ and all asterisk forms are unaffected.
//   - A rejected delimiter is emitted with its markers intact. Dropping a
//     delimiter we refused to interpret would lose characters the model sent.
//
// Code spans are scanned as opaque tokens in the same pass. Rendering them in a
// separate earlier pass would leave ANSI escapes in the text that the emphasis
// scan then reads as flanking context, so `code`_x_ would misjudge its boundary.
const mdMaxDelimRun = 3

var mdBoldItalicStyle = lipgloss.NewStyle().Bold(true).Italic(true)

// styleInline renders inline code and emphasis for one line of prose.
func styleInline(line string, th theme.Theme) string {
	runes := []rune(line)
	var b strings.Builder
	b.Grow(len(line))
	for i := 0; i < len(runes); {
		switch runes[i] {
		case '`':
			if content, end, ok := scanCodeSpan(runes, i); ok {
				b.WriteString(th.Code.Render(content))
				i = end
				continue
			}
		case '*', '_':
			if styled, end, ok := scanEmphasis(runes, i, th); ok {
				b.WriteString(styled)
				i = end
				continue
			}
			// Emit the whole run so its trailing delimiters are not re-examined
			// as a fresh opener on the next iteration.
			run := mdDelimRun(runes, i)
			b.WriteString(string(runes[i : i+run]))
			i += run
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// scanCodeSpan matches a backtick-delimited span at i, returning its content
// and the index just past the closing run. An unterminated opener is not a
// code span.
func scanCodeSpan(runes []rune, i int) (string, int, bool) {
	open := mdDelimRun(runes, i)
	for j := i + open; j < len(runes); {
		if runes[j] != '`' {
			j++
			continue
		}
		closing := mdDelimRun(runes, j)
		if closing == open {
			return string(runes[i+open : j]), j + open, true
		}
		j += closing
	}
	return "", i, false
}

// scanEmphasis matches an emphasis span opening at start, returning the styled
// text and the index just past the closing delimiters.
func scanEmphasis(runes []rune, start int, th theme.Theme) (string, int, bool) {
	delim := runes[start]
	openRun := mdDelimRun(runes, start)
	if !mdCanOpen(runes, start, openRun, delim) {
		return "", 0, false
	}
	if delim == '_' && openRun >= 2 && mdLooksLikeDunder(runes, start+openRun) {
		return "", 0, false
	}

	for j := start + openRun; j < len(runes); {
		if runes[j] == '`' {
			if _, end, ok := scanCodeSpan(runes, j); ok {
				j = end
				continue
			}
		}
		if runes[j] != delim {
			j++
			continue
		}
		closeRun := mdDelimRun(runes, j)
		if !mdCanClose(runes, j, closeRun, delim) {
			j += closeRun
			continue
		}
		inner := string(runes[start+openRun : j])
		if strings.TrimSpace(inner) == "" {
			j += closeRun
			continue
		}
		used := min(openRun, closeRun, mdMaxDelimRun)
		// Delimiters the span did not consume stay literal: the leading ones
		// here, the trailing ones by resuming the outer scan before them.
		leftover := string(runes[start : start+openRun-used])
		return leftover + mdEmphasisStyle(used).Render(styleInline(inner, th)), j + used, true
	}
	return "", 0, false
}

// mdEmphasisStyle maps a consumed delimiter count to its text style.
func mdEmphasisStyle(used int) lipgloss.Style {
	switch used {
	case 1:
		return mdItalicStyle
	case 2:
		return mdBoldStyle
	default:
		return mdBoldItalicStyle
	}
}

// mdDelimRun returns the length of the run of identical delimiters at i.
func mdDelimRun(runes []rune, i int) int {
	n := 0
	for i+n < len(runes) && runes[i+n] == runes[i] {
		n++
	}
	return n
}

// mdCanOpen reports whether the delimiter run at start can open emphasis.
// Underscore additionally may not open inside a word, which is what keeps
// snake_case identifiers and file paths intact.
func mdCanOpen(runes []rune, start, n int, delim rune) bool {
	left, right := mdFlanking(runes, start, n)
	if delim == '*' {
		return left
	}
	return left && (!right || mdIsPunct(runes, start-1))
}

// mdCanClose reports whether the delimiter run at start can close emphasis.
func mdCanClose(runes []rune, start, n int, delim rune) bool {
	left, right := mdFlanking(runes, start, n)
	if delim == '*' {
		return right
	}
	return right && (!left || mdIsPunct(runes, start+n))
}

// mdFlanking classifies the delimiter run at start as left- and/or
// right-flanking per CommonMark, judged by the characters bracketing the run.
func mdFlanking(runes []rune, start, n int) (left, right bool) {
	prevSpace, nextSpace := mdIsSpace(runes, start-1), mdIsSpace(runes, start+n)
	prevPunct, nextPunct := mdIsPunct(runes, start-1), mdIsPunct(runes, start+n)
	left = !nextSpace && (!nextPunct || prevSpace || prevPunct)
	right = !prevSpace && (!prevPunct || nextSpace || nextPunct)
	return left, right
}

// mdIsSpace treats the line boundary as whitespace, per CommonMark.
func mdIsSpace(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) {
		return true
	}
	return unicode.IsSpace(runes[i])
}

func mdIsPunct(runes []rune, i int) bool {
	if i < 0 || i >= len(runes) {
		return false
	}
	return unicode.IsPunct(runes[i]) || unicode.IsSymbol(runes[i])
}

// mdLooksLikeDunder reports whether an identifier ending in "__" starts at
// from, i.e. the text after an opening "__" completes a Python dunder such as
// __init__ or __main__ rather than a run of emphasized prose.
func mdLooksLikeDunder(runes []rune, from int) bool {
	if from >= len(runes) || !mdIsIdentStart(runes[from]) {
		return false
	}
	end := from
	for end < len(runes) && mdIsIdentPart(runes[end]) {
		end++
	}
	if end-from < 3 || runes[end-1] != '_' || runes[end-2] != '_' {
		return false
	}
	return end >= len(runes) || !mdIsIdentPart(runes[end])
}

func mdIsIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func mdIsIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
