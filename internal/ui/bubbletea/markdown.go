package bubbletea

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Lightweight markdown for assistant responses. This is deliberately a small
// in-house subset (headings, lists, fenced code, tables, inline bold/italic/code)
// rather than a full CommonMark renderer or a glamour dependency — enough to make
// typical model output readable in the TUI. Inline styling is applied per wrapped
// line, so a marker that straddles a wrap boundary degrades to literal text.
var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	mdBullet  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
	mdSepCell = regexp.MustCompile(`^\s*:?-{1,}:?\s*$`)
)

var (
	mdBoldStyle   = lipgloss.NewStyle().Bold(true)
	mdItalicStyle = lipgloss.NewStyle().Italic(true)
)

type colAlign int

const (
	alignLeft colAlign = iota
	alignCenter
	alignRight
)

type tableBlock struct {
	headers []string
	aligns  []colAlign
	rows    [][]string
}

// codeWrapHint is shown under a fenced block only after its closing fence, and
// only when a line in that block had to wrap. Emitting on the closer keeps the
// hint from flickering under a block that is still streaming.
const codeWrapHint = "wrapped to fit · /copy code for the exact text"

// renderMarkdown converts assistant text into styled, width-wrapped lines.
func renderMarkdown(text string, width int, th theme.Theme) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	inCode := false
	codeWrapped := false
	rawLines := strings.Split(text, "\n")
	for i := 0; i < len(rawLines); i++ {
		raw := rawLines[i]
		if strings.HasPrefix(strings.TrimSpace(raw), "```") {
			if inCode && codeWrapped {
				out = append(out, renderCodeWrapHint(width, th))
			}
			inCode = !inCode
			codeWrapped = false
			continue // hide the fence markers themselves
		}
		if inCode {
			rows := renderCodeLine(raw, width, th)
			if len(rows) > 1 {
				codeWrapped = true
			}
			out = append(out, rows...)
			continue
		}
		if isTableStart(rawLines, i) {
			tbl, nextI := collectTable(rawLines, i)
			out = append(out, renderTable(tbl, width, th)...)
			i = nextI - 1
			continue
		}
		out = append(out, renderProseLine(raw, width, th)...)
	}
	return out
}

// renderCodeWrapHint returns the dim pointer to /copy code, truncated so it
// cannot overflow a narrow terminal.
func renderCodeWrapHint(width int, th theme.Theme) string {
	return th.Dim.Render(truncateVisible(codeWrapHint, width))
}

// renderCodeLine renders a verbatim code line, wrapping (not truncating) so
// every character stays on screen. Indentation on the first visual row is
// preserved; continuation rows get no synthetic indent, because spaces we
// invent would corrupt a mouse-drag paste. There is no left-bar prefix: a
// copyable "│ " gutter made mouse-select paste unusable for commands the
// model asked the user to run. Theme.Code still distinguishes the block
// from prose.
func renderCodeLine(line string, width int, th theme.Theme) []string {
	return wrapVerbatimStyled(line, width, th.Code)
}

// renderProseLine handles headings, bullets, and paragraphs with inline styling.
func renderProseLine(line string, width int, th theme.Theme) []string {
	if m := mdHeading.FindStringSubmatch(line); m != nil {
		return wrapStyled(renderMathInProse(m[2]), width, th.Title)
	}
	prefix := ""
	body := line
	if m := mdBullet.FindStringSubmatch(line); m != nil {
		prefix = m[1] + "• "
		body = m[2]
	}
	body = renderMathInProse(body)
	pw := lipgloss.Width(prefix)
	wrapped := strings.Split(wrapText(body, max(width-pw, 1)), "\n")
	out := make([]string, 0, len(wrapped))
	for i, w := range wrapped {
		styled := styleInline(w, th)
		if i == 0 && prefix != "" {
			out = append(out, th.Accent.Render(prefix)+styled)
		} else if prefix != "" {
			out = append(out, strings.Repeat(" ", pw)+styled)
		} else {
			out = append(out, styled)
		}
	}
	return out
}

// wrapStyled wraps plain text and applies a single style to each line.
func wrapStyled(text string, width int, style lipgloss.Style) []string {
	wrapped := strings.Split(wrapText(text, width), "\n")
	out := make([]string, 0, len(wrapped))
	for _, w := range wrapped {
		out = append(out, style.Render(w))
	}
	return out
}

// truncateVisible cuts a string to at most width visible columns.
func truncateVisible(s string, width int) string {
	return ansi.Truncate(s, width, "")
}

// isTableStart reports whether line i is the header of a GFM table followed by
// a valid separator row at line i+1.
func isTableStart(lines []string, i int) bool {
	if i+1 >= len(lines) {
		return false
	}
	headerTrimmed := strings.TrimSpace(lines[i])
	if headerTrimmed == "" || !strings.Contains(headerTrimmed, "|") {
		return false
	}
	if strings.HasPrefix(headerTrimmed, "```") {
		return false
	}
	headerCells := splitTableRow(headerTrimmed)
	if len(headerCells) < 2 {
		return false
	}
	hasNonEmpty := false
	for _, c := range headerCells {
		if c != "" {
			hasNonEmpty = true
			break
		}
	}
	if !hasNonEmpty {
		return false
	}

	sepAligns, isSep := isTableSeparator(lines[i+1])
	if !isSep {
		return false
	}
	return len(sepAligns) >= 2
}

// isTableSeparator checks if a line is a GFM table delimiter (e.g. |---|:---:|---:|).
func isTableSeparator(line string) ([]colAlign, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "-") || !strings.Contains(trimmed, "|") {
		return nil, false
	}
	cells := splitTableRow(trimmed)
	if len(cells) < 2 {
		return nil, false
	}
	aligns := make([]colAlign, len(cells))
	for i, c := range cells {
		if !mdSepCell.MatchString(c) {
			return nil, false
		}
		cTrim := strings.TrimSpace(c)
		leftColon := strings.HasPrefix(cTrim, ":")
		rightColon := strings.HasSuffix(cTrim, ":")
		if leftColon && rightColon {
			aligns[i] = alignCenter
		} else if rightColon {
			aligns[i] = alignRight
		} else {
			aligns[i] = alignLeft
		}
	}
	return aligns, true
}

// splitTableRow splits a table row line by unescaped pipes and trims cell contents.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	const pipePlaceholder = "\x00"
	escaped := strings.ReplaceAll(line, `\|`, pipePlaceholder)

	escaped = strings.TrimPrefix(escaped, "|")
	escaped = strings.TrimSuffix(escaped, "|")

	parts := strings.Split(escaped, "|")
	cells := make([]string, len(parts))
	for i, p := range parts {
		cell := strings.ReplaceAll(p, pipePlaceholder, "|")
		cells[i] = strings.TrimSpace(cell)
	}
	return cells
}

// collectTable consumes a contiguous GFM table block starting at line i.
func collectTable(lines []string, i int) (tableBlock, int) {
	headerCells := splitTableRow(lines[i])
	aligns, _ := isTableSeparator(lines[i+1])

	var rows [][]string
	k := i + 2
	for k < len(lines) {
		line := strings.TrimSpace(lines[k])
		if line == "" || strings.HasPrefix(line, "```") {
			break
		}
		if !strings.Contains(line, "|") {
			break
		}
		rowCells := splitTableRow(line)
		if len(rowCells) == 0 {
			break
		}
		rows = append(rows, rowCells)
		k++
	}

	numCols := len(headerCells)
	if len(aligns) > numCols {
		numCols = len(aligns)
	}
	for _, r := range rows {
		if len(r) > numCols {
			numCols = len(r)
		}
	}

	normHeader := make([]string, numCols)
	copy(normHeader, headerCells)

	normAligns := make([]colAlign, numCols)
	copy(normAligns, aligns)

	normRows := make([][]string, len(rows))
	for rIdx, r := range rows {
		normRow := make([]string, numCols)
		copy(normRow, r)
		normRows[rIdx] = normRow
	}

	return tableBlock{
		headers: normHeader,
		aligns:  normAligns,
		rows:    normRows,
	}, k
}

// applyMathToTable rewrites LaTeX in headers and cells before width
// measurement so column budgets match the glyphs the user will see.
func applyMathToTable(tbl tableBlock) tableBlock {
	headers := make([]string, len(tbl.headers))
	for i, h := range tbl.headers {
		headers[i] = renderMathInProse(h)
	}
	rows := make([][]string, len(tbl.rows))
	for i, row := range tbl.rows {
		cells := make([]string, len(row))
		for j, c := range row {
			cells[j] = renderMathInProse(c)
		}
		rows[i] = cells
	}
	return tableBlock{headers: headers, aligns: tbl.aligns, rows: rows}
}

// renderTable formats a tableBlock into aligned, width-constrained lines.
func renderTable(tbl tableBlock, width int, th theme.Theme) []string {
	tbl = applyMathToTable(tbl)
	numCols := len(tbl.headers)
	if numCols == 0 {
		return nil
	}

	naturalWidths := make([]int, numCols)
	for c := 0; c < numCols; c++ {
		w := lipgloss.Width(tbl.headers[c])
		for _, row := range tbl.rows {
			if rw := lipgloss.Width(row[c]); rw > w {
				w = rw
			}
		}
		if w < 1 {
			w = 1
		}
		naturalWidths[c] = w
	}

	gutterWidth := (numCols - 1) * 3
	colWidths := calculateColumnWidths(naturalWidths, width, gutterWidth)

	var out []string
	out = append(out, renderTableRow(tbl.headers, colWidths, tbl.aligns, th, true)...)
	for _, row := range tbl.rows {
		out = append(out, renderTableRow(row, colWidths, tbl.aligns, th, false)...)
	}
	return out
}

// calculateColumnWidths allocates column widths so total table width fits within totalWidth.
func calculateColumnWidths(natural []int, totalWidth, gutterWidth int) []int {
	numCols := len(natural)
	colWidths := make([]int, numCols)
	copy(colWidths, natural)

	sumNatural := 0
	for _, w := range natural {
		sumNatural += w
	}

	availWidth := totalWidth - gutterWidth
	if availWidth <= 0 {
		availWidth = numCols
	}

	if sumNatural <= availWidth {
		return colWidths
	}

	targetTotal := availWidth
	minColWidth := 3
	if targetTotal/numCols < minColWidth {
		minColWidth = max(1, targetTotal/numCols)
	}

	for {
		sum := 0
		maxW := 0
		for _, w := range colWidths {
			sum += w
			if w > maxW {
				maxW = w
			}
		}
		if sum <= targetTotal || maxW <= minColWidth {
			break
		}

		for c := 0; c < numCols; c++ {
			if colWidths[c] == maxW && colWidths[c] > minColWidth {
				colWidths[c]--
				sum--
				if sum <= targetTotal {
					break
				}
			}
		}
	}

	return colWidths
}

// renderTableRow renders a single row of cells, wrapping cell contents to their
// respective column widths and joining them with dimmed gutters.
func renderTableRow(cells []string, colWidths []int, aligns []colAlign, th theme.Theme, isHeader bool) []string {
	numCols := len(colWidths)
	wrappedCols := make([][]string, numCols)
	maxLines := 1
	for c := 0; c < numCols; c++ {
		text := ""
		if c < len(cells) {
			text = cells[c]
		}
		wrapped := strings.Split(wrapText(text, colWidths[c]), "\n")
		if len(wrapped) > maxLines {
			maxLines = len(wrapped)
		}
		wrappedCols[c] = wrapped
	}

	var out []string
	gutter := " " + th.Dim.Render("|") + " "
	for lineIdx := 0; lineIdx < maxLines; lineIdx++ {
		cellParts := make([]string, numCols)
		for c := 0; c < numCols; c++ {
			piece := ""
			if lineIdx < len(wrappedCols[c]) {
				piece = wrappedCols[c][lineIdx]
			}
			styled := styleInline(piece, th)
			if isHeader {
				styled = th.Title.Render(styled)
			}
			visW := lipgloss.Width(styled)
			pad := colWidths[c] - visW
			if pad < 0 {
				styled = truncateVisible(styled, colWidths[c])
				pad = 0
			}

			var padded string
			align := alignLeft
			if c < len(aligns) {
				align = aligns[c]
			}
			switch align {
			case alignRight:
				padded = strings.Repeat(" ", pad) + styled
			case alignCenter:
				leftPad := pad / 2
				rightPad := pad - leftPad
				padded = strings.Repeat(" ", leftPad) + styled + strings.Repeat(" ", rightPad)
			default: // alignLeft
				padded = styled + strings.Repeat(" ", pad)
			}
			cellParts[c] = padded
		}
		out = append(out, strings.Join(cellParts, gutter))
	}
	return out
}
