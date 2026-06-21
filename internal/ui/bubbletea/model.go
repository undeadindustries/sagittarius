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

type model struct {
	opts ui.Options
	app  ui.App
	term *Terminal
	ctx  context.Context

	width  int
	height int

	viewport     viewport.Model
	input        textinput.Model
	status       ui.StatusBar
	idleStatus   ui.StatusBar
	output       strings.Builder
	busy         bool
	quitting     bool
	stream       <-chan ui.StreamEvent
	confirmReply chan bool

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

	vp := viewport.New(80, 20)
	vp.SetContent(welcomeText(opts))

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
		opts:          opts,
		app:           app,
		term:          term,
		input:         ti,
		viewport:      vp,
		status:        idleStatus,
		idleStatus:    idleStatus,
		suggestionIdx: -1,
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
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
		m.quitting = true
		return m, tea.Quit
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
		m.quitting = true
		return m, tea.Quit
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
		m.appendOutput(status + "\n")
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
			m.appendOutput("Providers dialog is unavailable in this session.\n")
			return
		}
		o := providersdialog.New(ctx, host.ProviderDialogDeps())
		o = o.SetSize(m.width, m.height)
		m.overlay = &o
	case ui.DialogModels:
		host, ok := m.app.(modelsDialogHost)
		if !ok {
			m.appendOutput("Models dialog is unavailable in this session.\n")
			return
		}
		o := modelsdialog.New(ctx, host.ModelsDialogDeps())
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
	header := renderHeader(m.opts, m.width)
	footer := renderFooter(m.status, m.width)
	inputLine := m.input.View()
	suggestions := m.renderSuggestions()

	bodyHeight := m.bodyHeight()
	m.viewport.Height = bodyHeight
	m.viewport.Width = max(m.width-2, 1)
	m.input.Width = max(m.width-lipgloss.Width(m.input.Prompt)-1, 1)

	sections := []string{header, m.viewport.View(), inputLine}
	if suggestions != "" {
		sections = append(sections, suggestions)
	}
	sections = append(sections, footer)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

var (
	suggestSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("254"))
	suggestDescStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	suggestMoreStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
)

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
			b.WriteString(suggestSelectedStyle.Render(row))
		} else {
			b.WriteString("  " + s.Label)
			if s.Description != "" {
				b.WriteString("  " + suggestDescStyle.Render(s.Description))
			}
		}
		b.WriteString("\n")
	}
	if more > 0 {
		b.WriteString(suggestMoreStyle.Render(fmt.Sprintf("  … %d more", more)))
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
			m.status.Left = "thinking…"
			return m, nil
		case "n", "N":
			m.confirmReply <- false
			m.confirmReply = nil
			m.status.Left = "thinking…"
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	if m.busy {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
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

	m.appendOutput("> " + line + "\n")
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
		m.appendOutput(ev.Text)
	case ui.StreamInfo:
		m.appendOutput(ev.Text)
	case ui.StreamQuit:
		m.busy = false
		return m, tea.Quit
	case ui.StreamOpenDialog:
		m.openDialog(ev.Dialog)
	case ui.StreamToolStart:
		m.appendOutput("[tool: " + ev.ToolName + "]\n")
	case ui.StreamToolConfirm:
		m.appendOutput("[confirm " + ev.ToolName + ": " + ev.Text + "] (y/n)\n")
		m.confirmReply = ev.ConfirmReply
		m.status.Left = "confirm tool"
	case ui.StreamToolResult:
		m.appendOutput("[tool result: " + ev.ToolName + " " + ev.Text + "]\n")
	case ui.StreamError:
		if ev.Err != nil {
			m.appendOutput("Error: " + ev.Err.Error() + "\n")
		} else if ev.Text != "" {
			m.appendOutput("Error: " + ev.Text + "\n")
		}
	case ui.StreamDone:
		m.busy = false
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

func (m *model) appendOutput(text string) {
	m.output.WriteString(text)
	m.syncViewportContent()
}

func (m *model) wrapWidth() int {
	w := m.viewport.Width
	if w <= 0 {
		w = max(m.width-2, 1)
	}
	return w
}

func (m *model) syncViewportContent() {
	m.viewport.SetContent(wrapText(m.output.String(), m.wrapWidth()))
	m.viewport.GotoBottom()
}

func (m *model) bodyHeight() int {
	chrome := 6 + m.suggestionRows()
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
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

func welcomeText(opts ui.Options) string {
	text := "Sagittarius — type a message and press Enter.\nUse /quit or Ctrl+C to exit.\n\n"
	if opts.Notice != "" {
		text += opts.Notice + "\n\n"
	}
	return text
}

func renderHeader(opts ui.Options, width int) string {
	title := opts.BannerTitle
	if title == "" {
		title = "Sagittarius"
	}
	line := title
	if opts.Version != "" {
		line += " " + opts.Version
	}
	style := lipgloss.NewStyle().Bold(true).Width(max(width, 1))
	return style.Render(line)
}

func renderFooter(status ui.StatusBar, width int) string {
	left := status.Left
	right := status.Right
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	line := left + strings.Repeat(" ", gap) + right
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Width(max(width, 1))
	if status.Detail != "" {
		line += "\n" + status.Detail
	}
	return style.Render(line)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
