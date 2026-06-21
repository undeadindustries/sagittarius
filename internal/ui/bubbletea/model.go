package bubbletea

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

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
}

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
		opts:       opts,
		app:        app,
		term:       term,
		input:      ti,
		viewport:   vp,
		status:     idleStatus,
		idleStatus: idleStatus,
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return textinput.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	header := renderHeader(m.opts, m.width)
	footer := renderFooter(m.status, m.width)
	inputLine := m.input.View()

	bodyHeight := m.bodyHeight()
	m.viewport.Height = bodyHeight
	m.viewport.Width = max(m.width-2, 1)
	m.input.Width = max(m.width-lipgloss.Width(m.input.Prompt)-1, 1)

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		m.viewport.View(),
		inputLine,
		footer,
	)
}

func (m *model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport.Width = max(msg.Width-2, 1)
	m.viewport.Height = m.bodyHeight()
	m.input.Width = max(msg.Width-lipgloss.Width(m.input.Prompt)-1, 1)
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
	case "enter":
		line := strings.TrimSpace(m.input.Value())
		m.input.SetValue("")
		return m, func() tea.Msg { return submitMsg{line: line} }
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
	m.viewport.SetContent(m.output.String())
	m.viewport.GotoBottom()
}

func (m *model) bodyHeight() int {
	const chrome = 6
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
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
