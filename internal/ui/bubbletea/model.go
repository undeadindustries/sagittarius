package bubbletea

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// providerDialogHost is implemented by an App that can supply the providers
// wizard dependencies (the agent App). The TUI never imports the agent package;
// it discovers the capability via this interface.
type providerDialogHost interface {
	ProviderDialogDeps() providersdialog.Deps
}

// modelsDialogHost is implemented by an App that can supply the models picker
// dependencies. Same capability-interface pattern as providerDialogHost.
type modelsDialogHost interface {
	ModelsDialogDeps() modelsdialog.Deps
}

type streamEventMsg struct {
	event ui.StreamEvent
}

type statusMsg struct {
	status ui.StatusBar
}

type submitMsg struct {
	line string
}

// scrollRole classifies a scrollback block so the renderer can apply a
// consistent prefix glyph and color per message kind.
type scrollRole int

const (
	roleUser scrollRole = iota
	roleResponse
	roleInfo
	roleError
	roleToolStart
	roleToolResult
	roleConfirm
)

// scrollBlock is one logical message in the scrollback. text may contain
// embedded newlines; the renderer wraps and prefixes it at paint time.
type scrollBlock struct {
	role scrollRole
	text string
}

type model struct {
	opts ui.Options
	app  ui.App
	term *Terminal
	ctx  context.Context
	th   theme.Theme

	width  int
	height int

	viewport   viewport.Model
	input      textinput.Model
	status     ui.StatusBar
	idleStatus ui.StatusBar

	// welcome is the static banner/tips seeded above the scrollback.
	welcome string
	// blocks is the structured scrollback: each block carries a role so the
	// renderer can prefix and color it consistently (user, assistant, info,
	// error, tool lifecycle). Streamed assistant text accumulates into the
	// block at openResponseIdx until the turn ends.
	blocks          []scrollBlock
	openResponseIdx int

	busy     bool
	quitting bool
	// exitSummary is captured when quitting begins so the Terminal can print the
	// goodbye screen after the alt-screen program tears down.
	exitSummary string
	stream      <-chan ui.StreamEvent
	// confirmReply is set while a tool confirmation is pending; the confirm
	// band renders above the input until the user answers y/n.
	confirmReply chan bool
	confirmText  string

	// Slash-command autocompletion state.
	suggestions    []ui.Suggestion
	suggestionIdx  int // -1 means nothing highlighted (user is still typing)
	completionFrom int // byte offset in the input where the active token starts

	// overlay holds the active providers wizard. When non-nil it takes over
	// input and rendering until it reports Done.
	overlay *providersdialog.Model
	// modelsOverlay holds the active models picker (mutually exclusive with
	// overlay).
	modelsOverlay *modelsdialog.Model
}

// hasOverlay reports whether any modal dialog is currently active.
func (m *model) hasOverlay() bool {
	return m.overlay != nil || m.modelsOverlay != nil
}

// maxVisibleSuggestions caps the inline suggestion list height.
const maxVisibleSuggestions = 8

func newModel(opts ui.Options, app ui.App, term *Terminal) *model {
	ti := textinput.New()
	ti.Placeholder = "Type a message"
	ti.Focus()
	ti.CharLimit = 8192
	ti.Prompt = "> "

	th := theme.Resolve(opts.ThemeName, opts.NoColor)

	welcome := welcomeText(opts, th)
	vp := viewport.New(80, 20)
	vp.SetContent(welcome)

	idleStatus := opts.InitialStatus
	if idleStatus.Left == "" && idleStatus.Right == "" {
		idleStatus = ui.StatusBar{
			Left:  "ready",
			Right: "Ctrl+C or /quit to exit",
		}
	}
	if idleStatus.Right == "" {
		idleStatus.Right = "Ctrl+C or /quit to exit"
	}

	m := &model{
		opts:            opts,
		app:             app,
		term:            term,
		th:              th,
		welcome:         welcome,
		openResponseIdx: -1,
		input:           ti,
		viewport:        vp,
		status:          idleStatus,
		idleStatus:      idleStatus,
		suggestionIdx:   -1,
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

// beginQuit marks the session as quitting, captures the goodbye summary (so the
// Terminal can print it after teardown), and returns the quit command. Calling
// it more than once keeps the first captured summary.
func (m *model) beginQuit() tea.Cmd {
	if !m.quitting {
		m.quitting = true
		m.exitSummary = m.renderExitSummary()
	}
	return tea.Quit
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.hasOverlay() {
		return m.updateOverlay(msg)
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSize(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case submitMsg:
		return m.handleSubmit(msg.line)
	case streamEventMsg:
		return m.handleStream(msg.event)
	case statusMsg:
		m.status = msg.status
		return m, nil
	case tea.QuitMsg:
		return m, m.beginQuit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateOverlay forwards messages to the active dialog overlay. Stream events
// (e.g. the StreamDone that ends the slash turn) and window resizes are still
// handled by the host so the underlying session state stays consistent.
func (m *model) updateOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.overlay != nil {
			o := m.overlay.SetSize(msg.Width, msg.Height)
			m.overlay = &o
		}
		if m.modelsOverlay != nil {
			o := m.modelsOverlay.SetSize(msg.Width, msg.Height)
			m.modelsOverlay = &o
		}
		return m, nil
	case streamEventMsg:
		return m.handleStream(msg.event)
	case tea.QuitMsg:
		return m, m.beginQuit()
	}

	if m.overlay != nil {
		next, cmd := m.overlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.overlay = &next
		return m, cmd
	}

	next, cmd := m.modelsOverlay.Update(msg)
	if next.Done() {
		m.closeOverlay(next.Status())
		return m, cmd
	}
	m.modelsOverlay = &next
	return m, cmd
}

// closeOverlay removes any active dialog, surfaces its closing status, and
// resets the footer to the (possibly refreshed) idle status.
func (m *model) closeOverlay(status string) {
	m.overlay = nil
	m.modelsOverlay = nil
	if status != "" {
		m.addBlock(roleInfo, status)
	}
	m.refreshIdleStatus()
	m.status = m.idleStatus
}

func (m *model) openDialog(kind ui.DialogKind) {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	switch kind {
	case ui.DialogProviders:
		host, ok := m.app.(providerDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Providers dialog is unavailable in this session.")
			return
		}
		o := providersdialog.New(ctx, host.ProviderDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.overlay = &o
	case ui.DialogModels:
		host, ok := m.app.(modelsDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Models dialog is unavailable in this session.")
			return
		}
		o := modelsdialog.New(ctx, host.ModelsDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.modelsOverlay = &o
	}
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	if m.overlay != nil {
		return m.overlay.View()
	}
	if m.modelsOverlay != nil {
		return m.modelsOverlay.View()
	}
	header := renderHeader(m.opts, m.th, m.width)
	footer := renderFooter(m.statusWithMetrics(), m.th, m.width)
	inputLine := m.input.View()
	suggestions := m.renderSuggestions()

	bodyHeight := m.bodyHeight()
	m.viewport.Height = bodyHeight
	m.viewport.Width = max(m.width-2, 1)
	m.input.Width = max(m.width-lipgloss.Width(m.input.Prompt)-1, 1)

	sections := []string{header, m.viewport.View()}
	if band := m.renderConfirmBand(); band != "" {
		sections = append(sections, band)
	}
	sections = append(sections, inputLine)
	if suggestions != "" {
		sections = append(sections, suggestions)
	}
	sections = append(sections, footer)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderConfirmBand draws a focused panel above the input while a tool
// confirmation is pending, so the y/n prompt is not lost in scrollback.
func (m *model) renderConfirmBand() string {
	if m.confirmReply == nil {
		return ""
	}
	label := m.confirmText
	if label == "" {
		label = "Run tool?"
	}
	body := m.th.Accent.Render("Confirm: ") + m.th.Primary.Render(label) +
		"  " + m.th.Accent.Render("(y/n)")
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(max(m.width-2, 1))
	if m.th.Colored {
		style = style.BorderForeground(m.th.FocusBorderColor)
	}
	return style.Render(body)
}

// renderSuggestions draws the inline completion list, highlighting the selected
// row. It returns "" when there is nothing to show.
func (m *model) renderSuggestions() string {
	if len(m.suggestions) == 0 {
		return ""
	}
	limit := len(m.suggestions)
	more := 0
	if limit > maxVisibleSuggestions {
		more = limit - maxVisibleSuggestions
		limit = maxVisibleSuggestions
	}

	var b strings.Builder
	for i := 0; i < limit; i++ {
		s := m.suggestions[i]
		if i == m.suggestionIdx {
			row := "› " + s.Label
			if s.Description != "" {
				row += "  " + s.Description
			}
			b.WriteString(m.th.Selected.Render(row))
		} else {
			b.WriteString("  " + s.Label)
			if s.Description != "" {
				b.WriteString("  " + m.th.Secondary.Render(s.Description))
			}
		}
		b.WriteString("\n")
	}
	if more > 0 {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("  … %d more", more)))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport.Width = max(msg.Width-2, 1)
	m.viewport.Height = m.bodyHeight()
	m.input.Width = max(msg.Width-lipgloss.Width(m.input.Prompt)-1, 1)
	m.syncViewportContent()
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmReply != nil {
		switch msg.String() {
		case "y", "Y":
			m.confirmReply <- true
			m.confirmReply = nil
			m.confirmText = ""
			m.status.Left = "thinking…"
			return m, nil
		case "n", "N":
			m.confirmReply <- false
			m.confirmReply = nil
			m.confirmText = ""
			m.status.Left = "thinking…"
			return m, nil
		case "ctrl+c":
			return m, m.beginQuit()
		}
		return m, nil
	}

	if m.busy {
		switch msg.String() {
		case "ctrl+c":
			return m, m.beginQuit()
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, m.beginQuit()
	case "ctrl+shift+m":
		if cycler, ok := m.app.(interface {
			CycleInteractionMode(context.Context) (<-chan ui.StreamEvent, error)
		}); ok {
			ctx := m.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			events, err := cycler.CycleInteractionMode(ctx)
			if err != nil {
				_ = m.term.ShowError(err)
				return m, nil
			}
			m.busy = true
			m.status.Left = "mode"
			m.stream = events
			return m, waitStream(events)
		}
		return m, nil
	case "up":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(-1)
			return m, nil
		}
	case "down":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(1)
			return m, nil
		}
	case "tab":
		if len(m.suggestions) > 0 {
			idx := m.suggestionIdx
			if idx < 0 {
				idx = 0
			}
			m.acceptSuggestion(idx)
			return m, nil
		}
	case "esc":
		if len(m.suggestions) > 0 {
			m.clearSuggestions()
			return m, nil
		}
	case "enter":
		return m.handleEnter()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.refreshSuggestions()
	return m, cmd
}

// handleEnter accepts a highlighted suggestion (completing it, and submitting
// when it is a terminal command) or submits the typed line when nothing is
// highlighted.
func (m *model) handleEnter() (tea.Model, tea.Cmd) {
	if m.suggestionIdx >= 0 && m.suggestionIdx < len(m.suggestions) {
		s := m.suggestions[m.suggestionIdx]
		m.acceptSuggestion(m.suggestionIdx)
		if s.AppendSpace {
			// Command expects a subcommand or argument: stay on the line so the
			// user can continue (suggestions were refreshed by acceptSuggestion).
			return m, nil
		}
	}
	line := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	m.clearSuggestions()
	return m, func() tea.Msg { return submitMsg{line: line} }
}

// refreshSuggestions recomputes the completion list from the current input. It
// is a no-op when the app provides no completer or the line is not a slash
// command. Selection resets to "typing" (no highlight) on every change.
func (m *model) refreshSuggestions() {
	m.clearSuggestions()
	completer, ok := m.app.(ui.Completer)
	if !ok {
		return
	}
	val := m.input.Value()
	if !strings.HasPrefix(val, "/") {
		return
	}
	res := completer.Complete(val)
	m.suggestions = res.Items
	m.completionFrom = res.ReplaceFrom
}

func (m *model) clearSuggestions() {
	m.suggestions = nil
	m.suggestionIdx = -1
}

// moveSuggestion advances the highlight by delta with wrap-around.
func (m *model) moveSuggestion(delta int) {
	n := len(m.suggestions)
	if n == 0 {
		return
	}
	if m.suggestionIdx < 0 {
		if delta > 0 {
			m.suggestionIdx = 0
		} else {
			m.suggestionIdx = n - 1
		}
		return
	}
	m.suggestionIdx = (m.suggestionIdx + delta%n + n) % n
}

// acceptSuggestion replaces the active token with the chosen suggestion and
// refreshes the list (so a completed parent reveals its subcommands/args).
func (m *model) acceptSuggestion(i int) {
	if i < 0 || i >= len(m.suggestions) {
		return
	}
	s := m.suggestions[i]
	val := m.input.Value()
	from := m.completionFrom
	if from < 0 || from > len(val) {
		from = len(val)
	}
	newVal := val[:from] + s.Insert
	if s.AppendSpace {
		newVal += " "
	}
	m.input.SetValue(newVal)
	m.input.CursorEnd()
	m.refreshSuggestions()
}

func (m *model) handleSubmit(line string) (tea.Model, tea.Cmd) {
	if line == "" {
		return m, nil
	}

	m.addBlock(roleUser, line)
	m.busy = true
	m.status.Left = "thinking…"

	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	events, err := m.app.HandleInput(ctx, line)
	if err != nil {
		m.busy = false
		m.status = m.idleStatus
		_ = m.term.ShowError(err)
		return m, nil
	}
	m.stream = events
	return m, waitStream(events)
}

func (m *model) handleStream(ev ui.StreamEvent) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case ui.StreamTextDelta:
		m.addResponseDelta(ev.Text)
	case ui.StreamInfo:
		m.addBlock(roleInfo, ev.Text)
	case ui.StreamQuit:
		m.busy = false
		return m, m.beginQuit()
	case ui.StreamOpenDialog:
		m.openDialog(ev.Dialog)
	case ui.StreamToolStart:
		m.addBlock(roleToolStart, ev.ToolName)
	case ui.StreamToolConfirm:
		text := ev.ToolName
		if ev.Text != "" {
			text += ": " + ev.Text
		}
		m.addBlock(roleConfirm, text)
		m.confirmReply = ev.ConfirmReply
		m.confirmText = text
		m.status.Left = "confirm tool"
	case ui.StreamToolResult:
		text := ev.ToolName
		if ev.Text != "" {
			text += " " + ev.Text
		}
		m.addBlock(roleToolResult, text)
	case ui.StreamError:
		if ev.Err != nil {
			m.addBlock(roleError, ev.Err.Error())
		} else if ev.Text != "" {
			m.addBlock(roleError, ev.Text)
		}
	case ui.StreamDone:
		m.busy = false
		m.closeResponse()
		m.refreshIdleStatus()
		m.status = m.idleStatus
		m.stream = nil
		return m, nil
	}
	if m.stream != nil {
		return m, waitStream(m.stream)
	}
	return m, nil
}

// refreshIdleStatus pulls the latest status bar from the app when it exposes
// one, so the footer reflects mid-session changes (e.g. interaction mode and
// model after /mode or Ctrl+Shift+M) instead of the value captured at startup.
// Apps that do not expose Status keep the original idle status.
func (m *model) refreshIdleStatus() {
	provider, ok := m.app.(interface{ Status() ui.StatusBar })
	if !ok {
		return
	}
	s := provider.Status()
	if s.Left == "" && s.Right == "" {
		return
	}
	if s.Right == "" {
		s.Right = m.idleStatus.Right
	}
	m.idleStatus = s
}

// addBlock appends a discrete (non-streaming) scrollback block and closes any
// open assistant response so the next text delta starts a fresh block.
func (m *model) addBlock(role scrollRole, text string) {
	m.blocks = append(m.blocks, scrollBlock{role: role, text: strings.TrimRight(text, "\n")})
	m.openResponseIdx = -1
	m.syncViewportContent()
}

// addResponseDelta accumulates streamed assistant text into the current
// response block, starting one if none is open.
func (m *model) addResponseDelta(text string) {
	if m.openResponseIdx < 0 {
		m.blocks = append(m.blocks, scrollBlock{role: roleResponse})
		m.openResponseIdx = len(m.blocks) - 1
	}
	m.blocks[m.openResponseIdx].text += text
	m.syncViewportContent()
}

// closeResponse ends the current assistant response block (end of turn).
func (m *model) closeResponse() {
	m.openResponseIdx = -1
}

func (m *model) wrapWidth() int {
	w := m.viewport.Width
	if w <= 0 {
		w = max(m.width-2, 1)
	}
	return w
}

func (m *model) syncViewportContent() {
	m.viewport.SetContent(m.renderScrollback(m.wrapWidth()))
	m.viewport.GotoBottom()
}

// renderScrollback paints the welcome banner plus every block with its role's
// prefix and color, wrapped to width. Wrapping runs on the plain text before
// styling so embedded ANSI codes never throw off the wrap math.
func (m *model) renderScrollback(width int) string {
	lines := make([]string, 0, len(m.blocks)+4)
	if m.welcome != "" {
		lines = append(lines, strings.Split(strings.TrimRight(m.welcome, "\n"), "\n")...)
	}
	for _, blk := range m.blocks {
		lines = append(lines, m.renderBlock(blk, width)...)
	}
	return strings.Join(lines, "\n")
}

// roleStyle returns the prefix glyph plus the prefix and body styles for a role.
func (m *model) roleStyle(role scrollRole) (glyph string, prefix, body lipgloss.Style) {
	switch role {
	case roleUser:
		return "> ", m.th.Accent, m.th.Primary
	case roleResponse:
		return "✦ ", m.th.Accent, m.th.Response
	case roleInfo:
		return "ℹ ", m.th.Secondary, m.th.Secondary
	case roleError:
		return "✕ ", m.th.Error, m.th.Error
	case roleToolStart:
		return "⚙ ", m.th.Secondary, m.th.Secondary
	case roleToolResult:
		return "↳ ", m.th.Dim, m.th.Dim
	case roleConfirm:
		return "? ", m.th.Accent, m.th.Warning
	default:
		return "  ", m.th.Primary, m.th.Primary
	}
}

// renderBlock wraps a block's text and prefixes the first visual line with the
// role glyph; continuation lines are indented to align under it. Assistant
// responses are run through the lightweight markdown renderer; all other roles
// render as plain styled text.
func (m *model) renderBlock(blk scrollBlock, width int) []string {
	glyph, prefix, body := m.roleStyle(blk.role)
	gw := lipgloss.Width(glyph)
	indent := strings.Repeat(" ", gw)

	var rendered []string
	if blk.role == roleResponse {
		rendered = renderMarkdown(blk.text, max(width-gw, 1), m.th)
	} else {
		for _, line := range strings.Split(wrapText(blk.text, max(width-gw, 1)), "\n") {
			rendered = append(rendered, body.Render(line))
		}
	}

	out := make([]string, 0, len(rendered))
	for i, line := range rendered {
		if i == 0 {
			out = append(out, prefix.Render(glyph)+line)
		} else {
			out = append(out, indent+line)
		}
	}
	return out
}

func (m *model) bodyHeight() int {
	chrome := 6 + m.suggestionRows() + m.confirmBandRows()
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
}

// confirmBandRows is the height of the confirm panel (bordered: 3 lines) while
// a tool confirmation is pending, else 0.
func (m *model) confirmBandRows() int {
	if m.confirmReply == nil {
		return 0
	}
	return 3
}

// suggestionRows is the number of terminal lines the suggestion block occupies,
// including the optional "… N more" line, so the viewport can shrink to fit.
func (m *model) suggestionRows() int {
	n := len(m.suggestions)
	if n == 0 {
		return 0
	}
	if n > maxVisibleSuggestions {
		return maxVisibleSuggestions + 1
	}
	return n
}

func waitStream(events <-chan ui.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return streamEventMsg{event: ui.StreamEvent{Type: ui.StreamDone}}
		}
		return streamEventMsg{event: ev}
	}
}

func renderHeader(opts ui.Options, th theme.Theme, width int) string {
	title := opts.BannerTitle
	if title == "" {
		title = "Sagittarius"
	}
	line := title
	if opts.Version != "" {
		line += " " + opts.Version
	}
	return th.Title.Width(max(width, 1)).Render(line)
}

// statusWithMetrics augments the footer's right side with live context usage
// (and a compact token total on wide terminals) when the app exposes metrics
// and a context limit is known. It never mutates the stored status.
func (m *model) statusWithMetrics() ui.StatusBar {
	status := m.status
	mp, ok := m.app.(ui.MetricsProvider)
	if !ok {
		return status
	}
	stats := mp.SessionMetrics()
	pct := stats.ContextPercent()
	if pct < 0 {
		return status
	}
	usage := fmt.Sprintf("%d%% context", pct)
	// Include a compact token total only when the terminal is wide enough.
	if m.width >= 80 && stats.OutputTokens > 0 {
		usage = fmt.Sprintf("%s · %s out", usage, compactCount(stats.OutputTokens))
	}
	if status.Right != "" {
		status.Right = status.Right + "  ·  " + usage
	} else {
		status.Right = usage
	}
	return status
}

// compactCount formats a token count compactly (e.g. 1234 -> "1.2k").
func compactCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func renderFooter(status ui.StatusBar, th theme.Theme, width int) string {
	left := status.Left
	right := status.Right
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	line := left + strings.Repeat(" ", gap) + right
	if status.Detail != "" {
		line += "\n" + status.Detail
	}
	return th.Secondary.Width(max(width, 1)).Render(line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
