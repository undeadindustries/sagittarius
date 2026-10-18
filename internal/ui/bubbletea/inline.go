package bubbletea

import (
	"fmt"
	"log/slog"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/screendialog"
)

// inlinePrintedMsg is the ack for one tea.Println. welcome marks the startup
// banner. blocks is how many scrollback blocks that print committed.
type inlinePrintedMsg struct {
	welcome bool
	blocks  int
	gen     uint64
}

// sshCopyTipSavedMsg is the result of persisting ui.sshCopyTipShown.
type sshCopyTipSavedMsg struct{ err error }

// screenLayoutSavedMsg is the result of persisting ui.screen from the dialog.
type screenLayoutSavedMsg struct {
	mode string
	err  error
}

const (
	inlineOSC52Copy = "Sent with OSC 52. If it didn't arrive, select the text above and copy it with your terminal."
	inlineMouseHint = "Inline mode uses the terminal's own mouse. /screen fullscreen brings back in-app selection."
	inlineSavedInfo = "Inline mode is on. New SSH sessions start inline. /screen fullscreen switches this session back. Screen layout in /settings changes the default."
	inlineShortcuts = sessionHints
)

// inlineBanner is the welcome text, printed once into the terminal scrollback
// when the session starts inline. Empty when there is nothing to say.
func (m *model) inlineBanner() string {
	return strings.TrimRight(m.welcome, "\n")
}

func (m *model) ackInline(welcome bool, blocks int) tea.Cmd {
	gen := m.printGeneration
	return func() tea.Msg {
		return inlinePrintedMsg{welcome: welcome, blocks: blocks, gen: gen}
	}
}

func (m *model) handleInlinePrinted(msg inlinePrintedMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.printGeneration {
		return m, nil
	}
	m.printInFlight = false
	if msg.welcome {
		m.welcomePending = false
	} else {
		m.committed += msg.blocks
		if msg.blocks > 0 {
			m.usedInline = true
		}
	}
	if m.inlineQuit {
		return m, m.flushInlineAndQuit()
	}
	if m.pendingFullscreen && !m.hasOverlay() {
		return m, m.enterFullscreenNow()
	}
	if m.deferAltScreen && m.hasOverlay() && !m.inlineAlt {
		m.deferAltScreen = false
		m.inlineAlt = true
		return m, tea.EnterAltScreen
	}
	return m, m.inlineCommitCmd()
}

// screenFollowUp runs after every Update. Dialogs borrow the alternate screen,
// newly final blocks print in order, and a layout change waits out an
// in-flight print so a Println never lands while the alternate screen is up.
func (m *model) screenFollowUp(wasInline, wasOverlay bool) tea.Cmd {
	now := m.hasOverlay()
	switch {
	case !wasOverlay && now && m.inline && !m.inlineAlt:
		if m.printInFlight {
			m.deferAltScreen = true
			return nil
		}
		m.inlineAlt = true
		return tea.EnterAltScreen
	case wasOverlay && !now && m.pendingFullscreen:
		return m.enterFullscreenNow()
	case wasOverlay && !now && m.inline:
		m.inlineAlt = false
		m.deferAltScreen = false
		m.mouseEnabled = false
		m.clearSelection()
		if m.printInFlight {
			return nil
		}
		if m.inlineBootPending && m.width > 0 {
			boot := m.beginInlineSession()
			return tea.Sequence(tea.ExitAltScreen, boot)
		}
		return m.exitAltThenCommit()
	case m.pendingFullscreen && !m.printInFlight && !now:
		return m.enterFullscreenNow()
	case m.inline && !now:
		return m.inlineCommitCmd()
	default:
		return nil
	}
}

// inlineFollowUp is the overlay half of screenFollowUp, kept for tests that
// drive dialog edges without a layout change. wasInline is the current mode.
func (m *model) inlineFollowUp(wasOverlay bool) tea.Cmd {
	return m.screenFollowUp(m.inline, wasOverlay)
}

// beginInlineSession prints the welcome banner, then commits finished blocks.
// It runs once, after the first window size, and only for sessions that
// started inline.
func (m *model) beginInlineSession() tea.Cmd {
	m.inlineBootPending = false
	if text := strings.TrimRight(m.inlineBanner(), "\n"); strings.TrimSpace(text) != "" {
		m.welcomePending = true
		m.printInFlight = true
		m.usedInline = true
		return tea.Sequence(tea.Println(text), m.ackInline(true, 0))
	}
	return m.inlineCommitCmd()
}

// requestInline switches this session to inline. An open dialog only flips
// state; the dialog-close path leaves the alternate screen and prints.
func (m *model) requestInline() tea.Cmd {
	m.pendingFullscreen = false
	if m.inline {
		return nil
	}
	m.inline = true
	m.usedInline = true
	if m.hasOverlay() {
		m.mouseEnabled = false
		return nil
	}
	if m.width == 0 {
		m.inlineBootPending = true
		m.mouseEnabled = false
		return tea.Sequence(tea.ExitAltScreen, tea.DisableMouse)
	}
	return m.beginInlineSwitch()
}

// requestFullscreen switches this session back to the alternate screen. A
// print that is already queued has to finish first: Bubble Tea drops a
// Println that arrives while the alternate screen is active.
func (m *model) requestFullscreen() tea.Cmd {
	if !m.inline && !m.pendingFullscreen {
		return nil
	}
	if m.hasOverlay() || m.printInFlight {
		m.pendingFullscreen = true
		return nil
	}
	return m.enterFullscreenNow()
}

// applyScreenMode handles /screen. "show" reports the current layout.
func (m *model) applyScreenMode(mode string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "show":
		m.addBlock(roleInfo, m.screenSessionLine())
		return nil
	case config.UIScreenInline:
		return m.requestInline()
	case config.UIScreenFullscreen, "full":
		return m.requestFullscreen()
	default:
		m.addBlock(roleInfo, "Usage: /screen <fullscreen|inline>")
		return nil
	}
}

func (m *model) screenSessionLine() string {
	if m.inline {
		return "This session is inline."
	}
	return "This session is full screen."
}

// beginInlineSwitch leaves the alternate screen, drops mouse capture, and
// prints every final block not yet in the scrollback. printInFlight stays set
// until the ack, even when there is nothing to print, so a later Println
// cannot land before the screen change.
func (m *model) beginInlineSwitch() tea.Cmd {
	m.inline = true
	m.usedInline = true
	m.mouseEnabled = false
	m.clearSelection()
	m.pendingFullscreen = false
	return tea.Sequence(m.inlineSwitchCmds()...)
}

// inlineSwitchCmds is the ordered command list for beginInlineSwitch.
// Exit comes before the print: Bubble Tea drops a Println while the
// alternate screen is still up.
func (m *model) inlineSwitchCmds() []tea.Cmd {
	cmds := []tea.Cmd{tea.ExitAltScreen, tea.DisableMouse}
	text, n, ok := m.takeInlinePrint()
	if !ok {
		m.printInFlight = true
		return append(cmds, m.ackInline(false, 0))
	}
	return append(cmds, tea.Println(text), m.ackInline(false, n))
}

func (m *model) enterFullscreenNow() tea.Cmd {
	m.inline = false
	m.pendingFullscreen = false
	m.inlineAlt = false
	m.deferAltScreen = false
	m.mouseEnabled = m.mouseWanted
	m.clearSelection()
	m.syncViewportContent()
	m.viewport.GotoBottom()
	if m.mouseWanted {
		return tea.Sequence(tea.EnterAltScreen, tea.EnableMouseCellMotion)
	}
	return tea.Sequence(tea.EnterAltScreen, tea.DisableMouse)
}

// exitAltThenCommit leaves the alternate screen before the next print. The
// commit command is built now, and the sequence sends the screen change first.
func (m *model) exitAltThenCommit() tea.Cmd {
	commit := m.inlineCommitCmd()
	if commit == nil {
		m.printInFlight = true
		commit = m.ackInline(false, 0)
	}
	return tea.Sequence(tea.ExitAltScreen, tea.DisableMouse, commit)
}

// flushScrollbackAndQuit prints blocks that never reached the native
// scrollback, then quits. Used when the session printed inline and later
// returned to full screen.
func (m *model) flushScrollbackAndQuit() tea.Cmd {
	text, n := m.inlineChunk(m.committed, false)
	m.committed = len(m.blocks)
	m.printInFlight = false
	if n == 0 || strings.TrimSpace(text) == "" {
		return tea.Sequence(tea.ExitAltScreen, tea.Quit)
	}
	return tea.Sequence(tea.ExitAltScreen, tea.Println(text), tea.Quit)
}

// takeInlinePrint reserves the next run of final blocks. ok is false when
// nothing can be printed yet (a print is in flight, the width is unknown, or
// no block is final).
func (m *model) takeInlinePrint() (text string, n int, ok bool) {
	if !m.inline || m.printInFlight || m.welcomePending || m.hasOverlay() || m.width == 0 {
		return "", 0, false
	}
	text, n = m.inlineChunk(m.committed, true)
	if n == 0 {
		return "", 0, false
	}
	m.printInFlight = true
	if strings.TrimSpace(text) == "" {
		m.printInFlight = false
		m.committed += n
		m.usedInline = true
		return m.takeInlinePrint()
	}
	return text, n, true
}

// inlineCommitCmd prints the next run of final blocks, one print at a time so
// consecutive updates cannot reorder them. Nothing prints before the first
// window size: width 0 would wrap to a guess.
func (m *model) inlineCommitCmd() tea.Cmd {
	text, n, ok := m.takeInlinePrint()
	if !ok {
		return nil
	}
	return tea.Sequence(tea.Println(text), m.ackInline(false, n))
}

// flushInlineAndQuit prints every block still on screen, final or not, then quits.
func (m *model) flushInlineAndQuit() tea.Cmd {
	text, n := m.inlineChunk(m.committed, false)
	m.committed = len(m.blocks)
	m.printInFlight = false
	m.welcomePending = false
	if n == 0 || strings.TrimSpace(text) == "" {
		return tea.Quit
	}
	return tea.Sequence(tea.Println(text), tea.Quit)
}

// inlineChunk renders blocks from index onward. When onlyFinal is set, it stops
// at the first block that can still change. The returned count is the number
// of blocks included. Spacing matches the full-screen scrollback: a blank line
// before each user block after the first.
func (m *model) inlineChunk(from int, onlyFinal bool) (string, int) {
	var b strings.Builder
	n := 0
	for i := from; i < len(m.blocks); i++ {
		if onlyFinal && !m.blockFinal(i) {
			break
		}
		text := inlinePrintRows(m.renderBlockLines(m.blocks[i], m.wrapWidth()))
		if n > 0 {
			if m.blocks[i].role == roleUser && i > 0 {
				b.WriteString("\n\n")
			} else {
				b.WriteString("\n")
			}
		}
		b.WriteString(text)
		n++
	}
	if n == 0 {
		return "", 0
	}
	return b.String(), n
}

// inlinePrintRows keeps the styled rows full screen would draw. A verbatim
// continuation is glued back onto the previous row so a code line is one
// physical line and the terminal wraps it. Prose stays word-wrapped.
func inlinePrintRows(lines []scrollLine) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			if line.verbatim && line.cont {
				b.WriteString(line.join)
				b.WriteString(line.text)
				continue
			}
			b.WriteByte('\n')
		}
		b.WriteString(line.text)
	}
	return b.String()
}

// blockFinal reports whether a block will not change again.
// A task group waits for a later block so its last card can still grow.
func (m *model) blockFinal(i int) bool {
	if i < 0 || i >= len(m.blocks) {
		return false
	}
	blk := m.blocks[i]
	switch blk.role {
	case roleUser, roleInfo, roleError:
		return true
	case roleResponse:
		return i != m.openResponseIdx
	case roleToolCard:
		return cardSettled(blk.card)
	case roleTaskGroup:
		if blk.taskGroup == nil || i == len(m.blocks)-1 {
			return false
		}
		for _, task := range blk.taskGroup.tasks {
			if !cardSettled(task) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func cardSettled(card *toolCard) bool {
	return card != nil && (card.phase == toolSuccess || card.phase == toolError)
}

func (m *model) inlineView() string {
	m.syncInputLayout()
	m.syncInputPlaceholder()

	live := strings.Join(tailLines(m.inlineLiveText(), m.bodyHeight()), "\n")
	footer := renderFooter(m.statusWithMetrics(), m.th, m.width)
	var sections []string
	if strings.TrimSpace(live) != "" {
		sections = append(sections, live)
	}
	sections = m.appendActivityAndHints(sections)
	sections = append(sections, m.renderInputSeparator(), m.renderInput())
	if suggestions := m.renderSuggestions(); suggestions != "" {
		sections = append(sections, suggestions)
	}
	sections = append(sections, footer)
	return clampWidth(lipgloss.JoinVertical(lipgloss.Left, sections...), m.width)
}

func (m *model) inlineLiveText() string {
	if m.committed >= len(m.blocks) {
		return ""
	}
	var lines []string
	for i := m.committed; i < len(m.blocks); i++ {
		for _, row := range m.renderBlockLines(m.blocks[i], m.wrapWidth()) {
			lines = append(lines, row.text)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) osc52CopyText() string {
	if m.inline {
		return inlineOSC52Copy
	}
	return fmt.Sprintf("Sent with OSC 52. If it didn't reach your clipboard, select with your terminal (%s), or run /screen inline.", clipboard.NativeSelectionHint(m.getenv))
}

func (m *model) osc52SilentHint(n int) string {
	if m.inline {
		return fmt.Sprintf("Sent via OSC 52 (%d chars). If nothing arrived, select it with the mouse.", n)
	}
	return fmt.Sprintf("Sent via OSC 52 (%d chars). Missing? Select with your terminal (%s), or /screen inline.", n, clipboard.NativeSelectionHint(m.getenv))
}

// noteSSHCopyTip arms the one-time dialog after an OSC 52 copy over SSH in
// full screen. The dialog itself opens later, once the UI is idle.
func (m *model) noteSSHCopyTip(msg clipboardResultMsg) {
	if m.inline || m.opts.ScreenReader || m.opts.SSHCopyTipShown || m.copyTipOpened || m.copyTipPending {
		return
	}
	if !msg.osc52 || msg.oscErr != nil || !clipboard.IsRemoteSession(m.getenv) {
		return
	}
	m.copyTipPending = true
}

func (m *model) maybeOpenCopyTip() tea.Cmd {
	if !m.copyTipPending || m.copyTipOpened || m.opts.SSHCopyTipShown {
		return nil
	}
	if m.busy || m.turnInFlight || m.hasOverlay() || m.quitting || m.width == 0 {
		return nil
	}
	m.copyTipPending = false
	m.copyTipOpened = true
	d := screendialog.New(clipboard.NativeSelectionHint(m.getenv))
	d = d.SetSize(m.width, m.height).SetTheme(m.th)
	m.screenOverlay = &d
	return m.markSSHCopyTipCmd()
}

func (m *model) markSSHCopyTipCmd() tea.Cmd {
	store, ok := m.app.(ui.ScreenPreferenceStore)
	if !ok {
		return nil
	}
	return func() tea.Msg {
		err := store.MarkSSHCopyTipShown()
		if err != nil {
			slog.Warn("ssh copy tip: save shown flag", "error", err)
		}
		return sshCopyTipSavedMsg{err: err}
	}
}

func (m *model) saveScreenLayoutCmd(mode string) tea.Cmd {
	store, ok := m.app.(ui.ScreenPreferenceStore)
	if !ok {
		return func() tea.Msg {
			return screenLayoutSavedMsg{mode: mode, err: fmt.Errorf("save screen layout: unavailable")}
		}
	}
	return func() tea.Msg {
		return screenLayoutSavedMsg{mode: mode, err: store.SaveScreenLayout(mode)}
	}
}

func (m *model) handleScreenLayoutSaved(msg screenLayoutSavedMsg) tea.Cmd {
	if msg.err != nil {
		m.addBlock(roleError, "Could not save the screen layout: "+msg.err.Error())
		return nil
	}
	if msg.mode == config.UIScreenAuto {
		m.addBlock(roleInfo, inlineSavedInfo)
	}
	return nil
}

func (m *model) updateScreenOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.screenOverlay.Update(msg)
	if !next.Done() {
		m.screenOverlay = &next
		return m, cmd
	}
	choice := next.Choice()
	m.screenOverlay = nil
	if choice != screendialog.ChoiceInline {
		return m, cmd
	}
	return m, tea.Batch(cmd, m.requestInline(), m.saveScreenLayoutCmd(config.UIScreenAuto))
}
