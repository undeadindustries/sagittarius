package toolsdialog

import "strings"

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// viewChromeLines counts every line in View() except the scrollable tool rows.
func (m Model) viewChromeLines() int {
	lines := 2 // lipgloss top + bottom border
	lines += 2 // "Tools" title + blank
	if m.info != "" {
		lines += 1 + lineCount(m.wrap(m.info))
	}
	if m.errMsg != "" {
		lines += 1 + lineCount(m.wrap("✗ "+m.errMsg))
	}
	lines += 1 // footer hint
	return lines
}

// visibleListRows returns how many inventory rows fit in the current terminal height.
func (m Model) visibleListRows() int {
	h := m.height
	if h <= 0 {
		return 15
	}
	total := len(m.rows)
	if total == 0 {
		return 0
	}
	chrome := m.viewChromeLines()
	for rows := total; rows >= 1; rows-- {
		section := rows
		if total > rows {
			section += 2 // scroll indicators (above + below)
		}
		if chrome+section <= h {
			return rows
		}
	}
	return 1
}

// listWindow returns the [start, end) slice of rows to render.
func (m Model) listWindow(total int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	vis := m.visibleListRows()
	if total <= vis {
		return 0, total
	}
	start = m.listOffset
	if start > total-vis {
		start = total - vis
	}
	if start < 0 {
		start = 0
	}
	end = start + vis
	if end > total {
		end = total
	}
	return start, end
}

func (m *Model) ensureListVisible() {
	total := len(m.rows)
	if total <= 0 {
		m.listOffset = 0
		return
	}
	vis := m.visibleListRows()
	if total <= vis {
		m.listOffset = 0
		return
	}
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	}
	if m.cursor >= m.listOffset+vis {
		m.listOffset = m.cursor - vis + 1
	}
	maxStart := total - vis
	if m.listOffset > maxStart {
		m.listOffset = maxStart
	}
	if m.listOffset < 0 {
		m.listOffset = 0
	}
}
