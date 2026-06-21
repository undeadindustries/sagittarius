package providersdialog

import "strings"

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// viewChromeLines counts every line in View() except the scrollable model rows.
func (m Model) viewChromeLines() int {
	lines := 2 // lipgloss top + bottom border
	lines += 2 // "Providers" header

	switch m.screen {
	case screenModels:
		lines += m.activationFixedLines()
	case screenAddModels:
		lines += m.addModelsFixedLines()
	default:
		lines += m.screenBodyFixedLines()
	}

	if m.info != "" {
		lines += 1 + lineCount(m.wrap(m.info))
	}
	if m.errMsg != "" {
		lines += 1 + lineCount(m.wrap("✗ "+m.errMsg))
	}
	lines += 1 + lineCount(m.wrap(m.footerHint()))
	return lines
}

func (m Model) activationFixedLines() int {
	if m.loading {
		return 2 + 1 // title block + status line
	}
	lines := 2 // screen title block
	if m.modelsErr != "" {
		lines += lineCount(m.wrap("✗ "+m.modelsErr))
		lines += 1 // blank after error block
		if len(m.models) == 0 {
			return lines + 1
		}
		lines += 1 + 1 // fallback hint + blank
	}
	if len(m.models) == 0 {
		return lines + 1
	}
	lines += 1 + 1 // activation hint + blank
	return lines
}

func (m Model) addModelsFixedLines() int {
	lines := 2 // title block
	if m.loading {
		return lines + 1
	}
	if m.modelsErr != "" {
		return lines + lineCount(m.wrap("✗ "+m.modelsErr)) + 1
	}
	if len(m.models) == 0 {
		return lines + 1
	}
	return lines
}

func (m Model) screenBodyFixedLines() int {
	// Non-scroll screens: the whole body is fixed; list rows are not subtracted.
	return 0
}

// visibleListRows returns how many list rows fit in the current terminal height.
func (m Model) visibleListRows() int {
	h := m.height
	if h <= 0 {
		return 15
	}
	total := len(m.models)
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

// listWindow returns the [start, end) slice of a long list to render.
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

func (m *Model) resetListScroll() {
	m.listOffset = 0
	m.cursor = 0
}

func (m *Model) moveListCursor(delta int) {
	n := m.listLen()
	if n <= 0 {
		return
	}
	if delta > 0 {
		m.cursor = wrapInc(m.cursor, n)
	} else {
		m.cursor = wrapDec(m.cursor, n)
	}
	if m.screenUsesListScroll() {
		m.ensureListVisible()
	}
}

func (m Model) screenUsesListScroll() bool {
	switch m.screen {
	case screenModels, screenAddModels:
		return true
	default:
		return false
	}
}

func (m *Model) ensureListVisible() {
	total := m.listLen()
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

func (m *Model) toggleAllChecked() {
	if len(m.checked) == 0 {
		return
	}
	all := true
	for _, c := range m.checked {
		if !c {
			all = false
			break
		}
	}
	for i := range m.checked {
		m.checked[i] = !all
	}
}
