package providersdialog

import "strings"

// visibleListRows returns how many list rows fit in the current terminal height.
func (m Model) visibleListRows() int {
	h := m.height
	if h <= 0 {
		return 15
	}
	reserved := 10 // title, activation header, footer, box border/padding
	if m.info != "" {
		reserved += strings.Count(m.wrap(m.info), "\n") + 2
	}
	if m.errMsg != "" {
		reserved += strings.Count(m.wrap(m.errMsg), "\n") + 2
	}
	rows := h - reserved
	if rows < 5 {
		return 5
	}
	return rows
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
