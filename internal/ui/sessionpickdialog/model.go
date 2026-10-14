package sessionpickdialog

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// Command lines the dialog submits. They match the slash command the user would
// type, so a picked row is indistinguishable from typing it.
const resumeCommandPrefix = "/chat resume "

// Refusal notices shown on the dialog instead of submitting a line.
const (
	noticeCurrentSession = "Already in this session."
	noticeLiveElsewhere  = "That session is open in another terminal, so it cannot be switched to here."
)

// rowKind distinguishes the two sections the menu lists.
type rowKind int

const (
	rowSession rowKind = iota
	rowCheckpoint
)

// row is one selectable entry in the flattened menu.
type row struct {
	kind    rowKind
	session SessionEntry
	tag     string
}

// loadMsg carries the off-UI-goroutine result of Deps.Load.
type loadMsg struct {
	catalog Catalog
	err     error
}

// Model is the session switcher overlay driven by the parent Bubble Tea model.
// When Done reports true the parent removes the overlay; SubmitLine then holds
// the /chat resume line to run, or "" when the user cancelled.
type Model struct {
	deps Deps
	ctx  context.Context
	th   theme.Theme
	now  func() time.Time

	width  int
	height int

	spin    spinner.Model
	loading bool
	loadErr string

	catalog Catalog
	rows    []row
	cursor  int

	done       bool
	submitLine string
	info       string
}

// New starts the switcher. The returned command loads the catalog and animates
// the spinner until it arrives, so opening the menu never blocks the UI.
func New(ctx context.Context, deps Deps) (Model, tea.Cmd) {
	m := Model{
		deps:    deps,
		ctx:     ctx,
		th:      theme.Default(),
		now:     time.Now,
		spin:    spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		loading: true,
	}
	return m, tea.Batch(m.spin.Tick, loadCmd(ctx, deps))
}

// loadCmd runs Deps.Load on a goroutine and delivers the result as a loadMsg.
func loadCmd(ctx context.Context, deps Deps) tea.Cmd {
	return func() tea.Msg {
		cat, err := deps.Load(ctx)
		return loadMsg{catalog: cat, err: err}
	}
}

// Done reports whether the dialog has finished and should be removed.
func (m Model) Done() bool { return m.done }

// SubmitLine returns the /chat resume line to run once the dialog closes, or
// "" when the user cancelled or picked nothing.
func (m Model) SubmitLine() string { return m.submitLine }

// SetSize informs the dialog of the terminal dimensions.
func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

// SetTheme applies the resolved color theme to the overlay.
func (m Model) SetTheme(th theme.Theme) Model {
	m.th = th
	return m
}

// Update advances the switcher for one message.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case loadMsg:
		return m.handleLoaded(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg), nil
	}
	return m, nil
}

// handleLoaded installs the catalog, or records the failure. Esc may have closed
// the dialog while the load was running; the parent then drops this message.
func (m Model) handleLoaded(msg loadMsg) Model {
	m.loading = false
	if msg.err != nil {
		m.loadErr = msg.err.Error()
		return m
	}
	m.catalog = msg.catalog
	m.rows = buildRows(msg.catalog)
	m.cursor = clampCursor(m.cursor, len(m.rows))
	return m
}

// handleKey is the whole keymap. Esc and q always close, even mid-load, so the
// menu is never a trap. Enter only submits a row that is actually switchable.
func (m Model) handleKey(key tea.KeyMsg) Model {
	switch key.String() {
	case "esc", "q":
		m.done = true
	case "up", "k":
		m.cursor = wrapDec(m.cursor, len(m.rows))
	case "down", "j":
		m.cursor = wrapInc(m.cursor, len(m.rows))
	case "enter":
		return m.selectCurrent()
	}
	return m
}

// selectCurrent submits the highlighted row, or explains why it cannot be
// switched to and stays open.
func (m Model) selectCurrent() Model {
	// A failed load keeps the error on screen: closing here would discard it.
	if m.loading || m.loadErr != "" {
		return m
	}
	if len(m.rows) == 0 {
		m.done = true
		return m
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return m
	}
	r := m.rows[m.cursor]
	switch r.kind {
	case rowSession:
		if r.session.ID == m.catalog.CurrentID {
			m.info = noticeCurrentSession
			return m
		}
		if r.session.Live {
			m.info = noticeLiveElsewhere
			return m
		}
		m.submitLine = resumeCommandPrefix + r.session.ID
	case rowCheckpoint:
		m.submitLine = resumeCommandPrefix + r.tag
	}
	m.done = true
	return m
}

// buildRows flattens the catalog into the order the menu shows: sessions in
// /chat list order (oldest first, matching the #N numbers), then checkpoints.
func buildRows(cat Catalog) []row {
	rows := make([]row, 0, len(cat.Sessions)+len(cat.Checkpoints))
	for _, s := range cat.Sessions {
		rows = append(rows, row{kind: rowSession, session: s})
	}
	for _, tag := range cat.Checkpoints {
		rows = append(rows, row{kind: rowCheckpoint, tag: tag})
	}
	return rows
}

// formatAge renders how long ago a session was last written, as the short form
// the menu shows. It is pure so it can be tested against a fixed clock.
func formatAge(then, now time.Time) string {
	if then.IsZero() {
		return ""
	}
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%d d ago", int(d/(24*time.Hour)))
	}
}

func clampCursor(i, n int) int {
	if n <= 0 {
		return 0
	}
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

func wrapInc(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i + 1) % n
}

func wrapDec(i, n int) int {
	if n <= 0 {
		return 0
	}
	return (i - 1 + n) % n
}
