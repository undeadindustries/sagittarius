package sessionpickdialog

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/overlay"
)

// Layout constants. chromeRows is the non-list height the frame, title, notice,
// and hint lines take, so the list window fits the terminal. The list never
// shrinks below minVisibleRows, even on a short terminal.
const (
	defaultVisibleRows = 10
	minVisibleRows     = 4
	maxVisibleRows     = 15
	chromeRows         = 14
	maxTitleRunes      = 48
	shortIDRunes       = 8
)

// View renders the session switcher overlay.
func (m Model) View() string {
	var b strings.Builder
	b.WriteString(overlay.Title(m.th, "Switch session") + "\n\n")
	b.WriteString(m.body())

	if m.info != "" {
		b.WriteString("\n\n" + m.th.Dim.Render(m.wrap(m.info)))
	}
	if m.loadErr != "" {
		b.WriteString("\n\n" + m.th.Error.Render(m.wrap("✗ "+m.loadErr)))
	}
	b.WriteString("\n\n" + overlay.Hints(m.th, "↑/↓ move • Enter switch • Esc cancel"))

	return overlay.Frame(m.th, m.width, overlay.DefaultMinWidth, b.String())
}

func (m Model) wrap(s string) string {
	return ui.WrapText(s, overlay.ContentWidth(m.width, overlay.DefaultMinWidth))
}

// body is the list area: a loading line, the empty state, or the scrolling menu.
func (m Model) body() string {
	if m.loading {
		return m.spin.View() + " " + m.th.Dim.Render("Loading sessions…")
	}
	if len(m.rows) == 0 {
		if m.loadErr != "" {
			return ""
		}
		return m.th.Dim.Render("No previous sessions or checkpoints.")
	}

	start, end := m.window()
	var b strings.Builder
	if start > 0 {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("… %d more", start)) + "\n")
	}
	for i := start; i < end; i++ {
		if i == start || m.rows[i].kind != m.rows[i-1].kind {
			b.WriteString(m.th.Title.Render(sectionTitle(m.rows[i].kind)) + "\n")
		}
		b.WriteString(overlay.Row(m.th, m.rowLabel(m.rows[i]), i == m.cursor) + "\n")
	}
	if end < len(m.rows) {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("… %d more", len(m.rows)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

// window returns the half-open range of rows to draw, keeping the cursor
// roughly centered so both directions have context.
func (m Model) window() (int, int) {
	visible := m.visibleRows()
	n := len(m.rows)
	if n <= visible {
		return 0, n
	}
	start := m.cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start > n-visible {
		start = n - visible
	}
	return start, start + visible
}

func (m Model) visibleRows() int {
	if m.height <= 0 {
		return defaultVisibleRows
	}
	return min(max(m.height-chromeRows, minVisibleRows), maxVisibleRows)
}

func (m Model) rowLabel(r row) string {
	if r.kind == rowCheckpoint {
		return r.tag
	}
	return m.sessionLabel(r.session)
}

func (m Model) sessionLabel(e SessionEntry) string {
	title := truncateRunes(e.Title, maxTitleRunes)
	if title == "" {
		title = truncateRunes(e.ID, shortIDRunes)
	}
	label := fmt.Sprintf("#%d  %s", e.Index, title)
	if meta := m.sessionMeta(e); meta != "" {
		label += m.th.Dim.Render("  " + meta)
	}
	switch {
	case e.ID == m.catalog.CurrentID:
		label += m.th.Dim.Render("  — current")
	case e.Live:
		label += m.th.Dim.Render("  — open in another terminal")
	}
	return label
}

// sessionMeta joins the age, size, and branch a user needs to tell similar
// sessions apart. Empty parts are dropped rather than rendered as gaps.
func (m Model) sessionMeta(e SessionEntry) string {
	parts := make([]string, 0, 3)
	if age := formatAge(e.LastUpdated, m.now()); age != "" {
		parts = append(parts, age)
	}
	parts = append(parts, fmt.Sprintf("%d msgs", e.MessageCount))
	if e.Branch != "" {
		parts = append(parts, e.Branch)
	}
	return strings.Join(parts, " · ")
}

func sectionTitle(k rowKind) string {
	if k == rowCheckpoint {
		return "Checkpoints"
	}
	return "Sessions"
}

// truncateRunes shortens s to at most n runes, marking the cut with an
// ellipsis. It works on runes so a multi-byte title is never split mid-character.
func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
