// Package mdfence locates CommonMark fenced code blocks in markdown text.
//
// It exists so the TUI markdown renderer (which must know which lines are
// verbatim) and the /copy code slash command (which extracts block bodies)
// share one definition of where a block starts and ends. Both previously
// toggled on any line starting with three backticks, which inverted on nested
// fences — a ```markdown block holding a ```php block rendered the php as
// prose — and had already drifted apart once on empty-fence handling.
//
// The rules follow CommonMark section 4.5: a fence is 3+ backticks or tildes
// indented at most 3 spaces; a closing fence uses the same character, is at
// least as long as the opener, and carries no info string, so an inner
// "```php" line can never close a block.
//
// One deliberate departure: models answering "write a GitHub issue I can
// paste" wrap the whole document in a ```markdown fence and use same-length
// fences inside it. Inside a block whose info string is markdown/md/gfm, an
// inner fence WITH an info string therefore opens a nested level (capped one
// deep), and a later bare fence closes that nested level before the outer
// one. Everything between the outer fences stays the outer block's raw body,
// so /copy code N copies the document with its inner fences intact.
package mdfence

import "strings"

// LineKind classifies a parsed line.
type LineKind int

const (
	// LineProse is ordinary markdown outside any fenced block.
	LineProse LineKind = iota
	// LineFence is the opening or closing fence marker of a top-level block.
	LineFence
	// LineCode is verbatim content of a block, including fence lines nested
	// inside a markdown-outer block.
	LineCode
)

// Line is one input line with its classification. Block is the index of the
// top-level block a LineCode line belongs to, or -1 (prose, fences, and code
// lines of dropped empty blocks).
type Line struct {
	Kind  LineKind
	Text  string
	Block int
}

// Block is a top-level fenced code block.
type Block struct {
	// Info is the trimmed info string after the opening fence ("" when none).
	Info string
	// Body is the raw content between the fences, lines joined with "\n".
	Body string
	// Closed is false when the text ended before a closing fence; the block
	// still yields the body captured so far.
	Closed bool
}

// IsMarkdownInfo reports whether an info string marks a block that wraps a
// markdown document — the shapes models use when fencing a whole reply for
// copying. Only these get the nested-fence departure described above.
func IsMarkdownInfo(info string) bool {
	switch strings.ToLower(strings.TrimSpace(info)) {
	case "markdown", "md", "gfm":
		return true
	}
	return false
}

// Parse splits text into classified lines and collects the top-level fenced
// blocks. Blocks whose body is empty or whitespace-only are dropped (they are
// not copyable content), and Line.Block is remapped to the kept blocks.
func Parse(text string) ([]Line, []Block) {
	rawLines := strings.Split(text, "\n")
	lines := make([]Line, 0, len(rawLines))
	var blocks []Block
	var stack []fenceLevel
	var body strings.Builder
	var outerInfo string

	appendBody := func(s string) {
		if body.Len() > 0 {
			body.WriteByte('\n')
		}
		body.WriteString(s)
	}

	for _, raw := range rawLines {
		raw = strings.TrimRight(raw, "\r")
		f, isFence := scanFence(raw)

		if len(stack) == 0 {
			if isFence {
				stack = append(stack, fenceLevel{char: f.char, length: f.length, indent: f.indent, blockIdx: len(blocks)})
				outerInfo = f.info
				body.Reset()
				lines = append(lines, Line{Kind: LineFence, Text: raw, Block: -1})
				continue
			}
			lines = append(lines, Line{Kind: LineProse, Text: raw, Block: -1})
			continue
		}

		if isFence {
			// A fence line closes the innermost level it can close. Closing
			// the outermost level ends the block; closing a nested level leaves
			// the line as raw content of the outer block.
			closeIdx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if closes(f, stack[i]) {
					closeIdx = i
					break
				}
			}
			if closeIdx == 0 {
				stack = stack[:0]
				blocks = append(blocks, Block{Info: outerInfo, Body: body.String(), Closed: true})
				body.Reset()
				lines = append(lines, Line{Kind: LineFence, Text: raw, Block: -1})
				continue
			}
			if closeIdx > 0 {
				stack = stack[:closeIdx]
				appendBody(stripIndent(raw, stack[0].indent))
				lines = append(lines, Line{Kind: LineCode, Text: raw, Block: stack[0].blockIdx})
				continue
			}
			// Not a closer. Inside a markdown-outer block an inner fence with
			// an info string opens a nested level (one deep); everywhere else
			// it is ordinary content, as CommonMark has no nested fences.
			if IsMarkdownInfo(outerInfo) && len(stack) == 1 && f.info != "" {
				stack = append(stack, fenceLevel{char: f.char, length: f.length, indent: f.indent, blockIdx: stack[0].blockIdx})
			}
			appendBody(stripIndent(raw, stack[0].indent))
			lines = append(lines, Line{Kind: LineCode, Text: raw, Block: stack[0].blockIdx})
			continue
		}

		appendBody(stripIndent(raw, stack[0].indent))
		lines = append(lines, Line{Kind: LineCode, Text: raw, Block: stack[0].blockIdx})
	}

	if len(stack) > 0 {
		blocks = append(blocks, Block{Info: outerInfo, Body: body.String(), Closed: false})
	}

	// Drop empty and whitespace-only bodies, remapping line block indices so
	// /copy code N and the renderer number the same blocks.
	var kept []Block
	remap := make([]int, len(blocks))
	for i, b := range blocks {
		if strings.TrimSpace(b.Body) == "" {
			remap[i] = -1
			continue
		}
		remap[i] = len(kept)
		kept = append(kept, b)
	}
	for i := range lines {
		if lines[i].Kind != LineCode {
			continue
		}
		if lines[i].Block >= 0 && remap[lines[i].Block] >= 0 {
			lines[i].Block = remap[lines[i].Block]
		} else {
			lines[i].Block = -1
		}
	}
	return lines, kept
}

// fenceLevel is one open fence on the stack. The stack holds the outer block
// plus at most one nested level inside a markdown-outer block; blockIdx is
// the index of the top-level block in the (pre-compaction) blocks slice and
// is only meaningful on the outermost level.
type fenceLevel struct {
	char     byte
	length   int
	indent   int
	blockIdx int
}

// fenceInfo describes a fence marker found on a line.
type fenceInfo struct {
	char   byte
	length int
	info   string
	indent int
}

// scanFence reports whether a line is a fence marker: 3+ backticks or tildes
// indented at most 3 spaces. A backtick fence whose info string contains a
// backtick is not a fence (CommonMark 4.5).
func scanFence(raw string) (fenceInfo, bool) {
	var fi fenceInfo
	indent := 0
	for indent < len(raw) && raw[indent] == ' ' {
		indent++
	}
	if indent > 3 || len(raw)-indent < 3 {
		return fi, false
	}
	rest := raw[indent:]
	c := rest[0]
	if c != '`' && c != '~' {
		return fi, false
	}
	n := 0
	for n < len(rest) && rest[n] == c {
		n++
	}
	if n < 3 {
		return fi, false
	}
	info := strings.TrimSpace(rest[n:])
	if c == '`' && strings.ContainsRune(info, '`') {
		return fi, false
	}
	return fenceInfo{char: c, length: n, info: info, indent: indent}, true
}

// closes reports whether a fence marker closes an open level: same character,
// at least as long as the opener, and no info string.
func closes(f fenceInfo, lvl fenceLevel) bool {
	return f.char == lvl.char && f.length >= lvl.length && f.info == ""
}

// stripIndent removes up to indent leading spaces, matching CommonMark's
// rule that content is dedented by the opening fence's indentation.
func stripIndent(s string, indent int) string {
	for indent > 0 && len(s) > 0 && s[0] == ' ' {
		s = s[1:]
		indent--
	}
	return s
}
