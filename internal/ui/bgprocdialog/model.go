package bgprocdialog

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type OutputProvider func(pid int) string
type KillProvider func(pid int) error

// Model is the background process viewer overlay.
type Model struct {
	processes []bgproc.Process
	list      list.Model
	outputVP  viewport.Model
	width     int
	height    int
	th        theme.Theme

	showOutput bool
	activePid  int

	outputProvider OutputProvider
	killProvider   KillProvider
}

type item struct {
	p bgproc.Process
}

func (i item) Title() string {
	cmd := i.p.Command
	if len(cmd) > 50 {
		cmd = cmd[:47] + "..."
	}
	return fmt.Sprintf("PID %d: %s", i.p.PID, cmd)
}

func (i item) Description() string {
	uptime := time.Since(i.p.StartedAt).Round(time.Second)
	status := string(i.p.Status)
	if i.p.Status == bgproc.StatusExited {
		status = fmt.Sprintf("exited (%d)", i.p.ExitCode)
	}
	return fmt.Sprintf("%s | Uptime: %s", status, uptime)
}

func (i item) FilterValue() string { return i.p.Command }

// New creates a new background process viewer overlay.
func New(width, height int, th theme.Theme, procs []bgproc.Process, op OutputProvider, kp KillProvider) *Model {
	items := make([]list.Item, 0, len(procs))
	for _, p := range procs {
		items = append(items, item{p})
	}
	l := list.New(items, list.NewDefaultDelegate(), width-4, height-6)
	l.Title = "Background Processes"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = th.Accent.Bold(true).Padding(0, 1)

	vp := viewport.New(width-4, height-6)

	return &Model{
		processes:      procs,
		list:           l,
		outputVP:       vp,
		width:          width,
		height:         height,
		th:             th,
		outputProvider: op,
		killProvider:   kp,
	}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd {
	return nil
}

type MsgDone struct{}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.showOutput {
			switch msg.String() {
			case "esc", "q", "left":
				m.showOutput = false
				return m, nil
			}
			var cmd tea.Cmd
			m.outputVP, cmd = m.outputVP.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return MsgDone{} }
		case "enter", "right":
			if i, ok := m.list.SelectedItem().(item); ok {
				m.activePid = i.p.PID
				m.showOutput = true
				out := m.outputProvider(m.activePid)
				if out == "" {
					out = "(No output or could not read log file)"
				}
				m.outputVP.SetContent(out)
				m.outputVP.GotoBottom()
				return m, nil
			}
		case "d", "delete", "backspace":
			if i, ok := m.list.SelectedItem().(item); ok && i.p.Status == bgproc.StatusRunning {
				_ = m.killProvider(i.p.PID)
				// Note: we don't remove from list, it just updates status next poll
				// But we could optimistically update the item here.
				i.p.Status = bgproc.StatusExited
				idx := m.list.Index()
				m.list.SetItem(idx, i)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.list.SetSize(msg.Width-4, msg.Height-6)
		m.outputVP.Width = msg.Width - 4
		m.outputVP.Height = msg.Height - 6
	}

	if !m.showOutput {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m *Model) View() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.th.Accent.GetForeground()).
		Padding(1, 2).
		Width(m.width - 2).
		Height(m.height - 2)

	var content string
	if m.showOutput {
		title := m.th.Accent.Bold(true).Render(fmt.Sprintf("Output for PID %d", m.activePid))
		hint := m.th.Dim.Render("Esc/Left to go back • Up/Down to scroll")
		content = title + "\n\n" + m.outputVP.View() + "\n\n" + hint
	} else {
		content = m.list.View()
		hint := m.th.Dim.Render("Enter/Right: view output • d/Delete: kill • Esc: close")
		content += "\n" + hint
	}

	return box.Render(content)
}
