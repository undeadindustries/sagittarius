package bubbletea

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelpickdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modesdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/onboardingdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/systempromptdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
	"github.com/undeadindustries/sagittarius/internal/ui/toolsdialog"
)

// providerDialogHost is implemented by an App that can supply the providers
// wizard dependencies (the agent App). The TUI never imports the agent package;
// it discovers the capability via this interface.
type providerDialogHost interface {
	ProviderDialogDeps() providersdialog.Deps
}

// modelsDialogHost is implemented by an App that can supply the per-model settings
// editor dependencies.
type modelsDialogHost interface {
	ModelsDialogDeps() modelsdialog.Deps
}

// modelPickDialogHost is implemented by an App that can supply the global model
// picker dependencies.
type modelPickDialogHost interface {
	ModelPickDialogDeps() modelpickdialog.Deps
}

// modesDialogHost is implemented by an App that can supply the modes-override
// editor dependencies.
type modesDialogHost interface {
	ModesDialogDeps() modesdialog.Deps
}

// systemPromptDialogHost is implemented by an App that can supply the project
// system-prompt picker dependencies.
type systemPromptDialogHost interface {
	SystemPromptDialogDeps() systempromptdialog.Deps
}

// mcpDialogHost is implemented by an App that can supply the MCP server wizard
// dependencies.
type mcpDialogHost interface {
	MCPDialogDeps() mcpdialog.Deps
}

// toolsDialogHost is implemented by an App that can supply the tool inventory
// dependencies.
type toolsDialogHost interface {
	ToolsDialogDeps() toolsdialog.Deps
}

// onboardingHost is implemented by an App that can supply first-run setup deps.
type onboardingHost interface {
	OnboardingDialogDeps() onboardingdialog.Deps
}

type streamEventMsg struct {
	event ui.StreamEvent
}

// clipboardResultMsg carries the outcome of an async clipboard copy started by
// copyToClipboard, so the blocking copy runs off the UI goroutine.
type clipboardResultMsg struct {
	text string
	err  error
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
	input      textarea.Model
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
	// turnCancel cancels the in-flight HandleInput turn; nil when no cancelable
	// turn is running. turnStart marks when the turn began, for the elapsed
	// timer in the working line. turnCanceled is true from the first Esc press
	// until StreamDone arrives or the stream is force-abandoned.
	// streamAbandoned is set by forceAbandonTurn so handleStream can discard
	// stale events that were already queued in the Bubble Tea message loop.
	turnCancel      context.CancelFunc
	turnStart       time.Time
	turnCanceled    bool
	streamAbandoned bool
	// spin drives the animated working/thinking indicator shown above the input
	// while a turn is in flight. It only ticks while busy.
	spin spinner.Model
	// working toggles visibility of the working indicator line; workingLabel is
	// the current activity (e.g. "Thinking…" or "Running write_file").
	working      bool
	workingLabel string
	// exitSummary is captured when quitting begins so the Terminal can print the
	// goodbye screen after the alt-screen program tears down.
	exitSummary string
	stream      <-chan ui.StreamEvent
	// confirmReply is set while a tool confirmation is pending; the confirm
	// band renders above the input until the user picks a choice. The decision
	// is once / session / deny.
	confirmReply chan ui.ConfirmDecision
	confirmText  string
	// confirmDiff is an optional unified-diff preview (write_file) shown in the
	// confirm band, colorized add/del.
	confirmDiff string
	// confirmChoice is the highlighted option in the confirm band
	// (0=once, 1=session, 2=no).
	confirmChoice int

	// Slash-command autocompletion state.
	suggestions    []ui.Suggestion
	suggestionIdx  int // -1 means nothing highlighted (user is still typing)
	completionFrom int // byte offset in the input where the active token starts

	// overlay holds the active providers wizard. When non-nil it takes over
	// input and rendering until it reports Done.
	overlay *providersdialog.Model
	// modelsOverlay holds the per-model settings editor (mutually exclusive
	// with other overlays).
	modelsOverlay *modelsdialog.Model
	// modelPickOverlay holds the global {Provider}/{Model} current-model picker.
	modelPickOverlay *modelpickdialog.Model
	// modesOverlay holds the mode-override editor.
	modesOverlay *modesdialog.Model
	// systemPromptOverlay holds the project system-prompt preset picker.
	systemPromptOverlay *systempromptdialog.Model
	// mcpOverlay holds the MCP server management wizard.
	mcpOverlay *mcpdialog.Model
	// toolsOverlay holds the tool inventory.
	toolsOverlay *toolsdialog.Model
	// onboardingOverlay holds the first-run provider setup wizard.
	onboardingOverlay *onboardingdialog.Model
}

// hasOverlay reports whether any modal dialog is currently active.
func (m *model) hasOverlay() bool {
	return m.onboardingOverlay != nil || m.overlay != nil ||
		m.modelsOverlay != nil || m.modelPickOverlay != nil || m.modesOverlay != nil ||
		m.systemPromptOverlay != nil || m.mcpOverlay != nil || m.toolsOverlay != nil
}

// maxVisibleSuggestions caps the inline suggestion list height.
const maxVisibleSuggestions = 8

func newModel(opts ui.Options, app ui.App, term *Terminal) *model {
	ti := textarea.New()
	ti.Placeholder = "Type a message"
	ti.Focus()
	ti.CharLimit = 8192
	ti.ShowLineNumbers = false
	ti.MaxHeight = maxInputRows
	// Enter submits the line; newlines are inserted with Alt/Shift+Enter (or
	// Ctrl+J) so multi-line prompts are still possible without losing the
	// single-key submit affordance.
	ti.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"))

	th := theme.Resolve(opts.ThemeName, opts.NoColor)
	applyInputTheme(&ti, th)

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
		spin:            newWorkingSpinner(th),
		status:          idleStatus,
		idleStatus:      idleStatus,
		suggestionIdx:   -1,
	}
	m.syncInputPrompt(idleStatus.Mode)

	// Seed initial scrollback (e.g. from --resume) so the user can see the
	// prior conversation immediately, not just have it silently in context.
	for _, entry := range opts.InitialScrollback {
		m.addBlock(scrollbackRoleToRole(entry.Role), entry.Text)
	}
	if len(opts.InitialScrollback) > 0 {
		m.viewport.SetContent(m.renderScrollback(m.wrapWidth()))
		m.viewport.GotoBottom()
	}

	return m
}

// applyInputTheme restyles the chat input box for th: a colored background on
// color themes, or reverse-video on greyscale (no color codes). It gives the
// typing zone a visible affordance, like Gemini CLI's grey input area.
func applyInputTheme(ti *textarea.Model, th theme.Theme) {
	if th.Colored {
		inputBg := lipgloss.NewStyle().Background(th.InputBg)
		ti.FocusedStyle.Base = inputBg
		ti.FocusedStyle.Text = inputBg
		ti.FocusedStyle.CursorLine = inputBg
		ti.FocusedStyle.Prompt = inputBg.Foreground(th.Accent.GetForeground())
		ti.FocusedStyle.Placeholder = inputBg.Faint(true)
		return
	}
	rev := lipgloss.NewStyle().Reverse(true)
	ti.FocusedStyle.Base = rev
	ti.FocusedStyle.Text = rev
	ti.FocusedStyle.CursorLine = rev
	ti.FocusedStyle.Prompt = rev
	ti.FocusedStyle.Placeholder = rev.Faint(true)
}

// setTheme switches the live UI theme by name ("default"|"greyscale") and
// re-derives the cached input/spinner/welcome styling, then repaints. The name
// is an explicit in-session choice, so NO_COLOR (a startup-only signal) is not
// re-applied here.
func (m *model) setTheme(name string) {
	m.th = theme.Resolve(name, false)
	applyInputTheme(&m.input, m.th)
	m.spin = newWorkingSpinner(m.th)
	m.welcome = welcomeText(m.opts, m.th)
	m.syncViewportContent()
}

func (m *model) Init() tea.Cmd {
	if m.opts.NeedsOnboarding {
		m.openOnboarding()
	}
	return textarea.Blink
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
	case clipboardResultMsg:
		return m, m.handleClipboardResult(msg)
	case statusMsg:
		m.status = msg.status
		return m, nil
	case spinner.TickMsg:
		// Keep the working indicator animating for the whole busy period; the
		// chain self-perpetuates via spinner.Update and dies once idle. Each tick
		// also advances the spinner's gradient color (no-op on greyscale).
		if !m.busy {
			return m, nil
		}
		applySpinnerColor(&m.spin, m.th)
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.QuitMsg:
		return m, m.beginQuit()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncInputLayout()
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
		if m.onboardingOverlay != nil {
			o := m.onboardingOverlay.SetSize(msg.Width, msg.Height)
			m.onboardingOverlay = &o
		}
		if m.overlay != nil {
			o := m.overlay.SetSize(msg.Width, msg.Height)
			m.overlay = &o
		}
		if m.modelsOverlay != nil {
			o := m.modelsOverlay.SetSize(msg.Width, msg.Height)
			m.modelsOverlay = &o
		}
		if m.modelPickOverlay != nil {
			o := m.modelPickOverlay.SetSize(msg.Width, msg.Height)
			m.modelPickOverlay = &o
		}
		if m.modesOverlay != nil {
			o := m.modesOverlay.SetSize(msg.Width, msg.Height)
			m.modesOverlay = &o
		}
		if m.systemPromptOverlay != nil {
			o := m.systemPromptOverlay.SetSize(msg.Width, msg.Height)
			m.systemPromptOverlay = &o
		}
		if m.mcpOverlay != nil {
			o := m.mcpOverlay.SetSize(msg.Width, msg.Height)
			m.mcpOverlay = &o
		}
		if m.toolsOverlay != nil {
			o := m.toolsOverlay.SetSize(msg.Width, msg.Height)
			m.toolsOverlay = &o
		}
		return m, nil
	case streamEventMsg:
		return m.handleStream(msg.event)
	case tea.QuitMsg:
		return m, m.beginQuit()
	}

	if m.onboardingOverlay != nil {
		next, cmd := m.onboardingOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.onboardingOverlay = &next
		return m, cmd
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

	if m.modelsOverlay != nil {
		next, cmd := m.modelsOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.modelsOverlay = &next
		return m, cmd
	}

	if m.modelPickOverlay != nil {
		next, cmd := m.modelPickOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.modelPickOverlay = &next
		return m, cmd
	}

	if m.modesOverlay != nil {
		next, cmd := m.modesOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.modesOverlay = &next
		return m, cmd
	}

	if m.systemPromptOverlay != nil {
		next, cmd := m.systemPromptOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.systemPromptOverlay = &next
		return m, cmd
	}

	if m.mcpOverlay != nil {
		next, cmd := m.mcpOverlay.Update(msg)
		if next.Done() {
			openTools := next.OpenTools()
			m.closeOverlay(next.Status())
			if openTools {
				m.openDialog(ui.DialogTools)
			}
			return m, cmd
		}
		m.mcpOverlay = &next
		return m, cmd
	}

	if m.toolsOverlay != nil {
		next, cmd := m.toolsOverlay.Update(msg)
		if next.Done() {
			openServers := next.OpenServers()
			m.closeOverlay(next.Status())
			if openServers {
				m.openDialog(ui.DialogMCP)
			}
			return m, cmd
		}
		m.toolsOverlay = &next
		return m, cmd
	}

	return m, nil
}

// closeOverlay removes any active dialog, surfaces its closing status, and
// resets the footer to the (possibly refreshed) idle status.
func (m *model) closeOverlay(status string) {
	m.onboardingOverlay = nil
	m.overlay = nil
	m.modelsOverlay = nil
	m.modelPickOverlay = nil
	m.modesOverlay = nil
	m.systemPromptOverlay = nil
	m.mcpOverlay = nil
	m.toolsOverlay = nil
	if status != "" {
		m.addBlock(roleInfo, status)
	}
	m.refreshIdleStatus()
	m.status = m.idleStatus
}

func (m *model) openOnboarding() {
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	host, ok := m.app.(onboardingHost)
	if !ok {
		m.addBlock(roleInfo, "First-run setup is unavailable in this session.")
		return
	}
	o := onboardingdialog.New(ctx, host.OnboardingDialogDeps())
	o = o.SetTheme(m.th)
	if m.width > 0 && m.height > 0 {
		o = o.SetSize(m.width, m.height)
	}
	m.onboardingOverlay = &o
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
			m.addBlock(roleInfo, "Models settings dialog is unavailable in this session.")
			return
		}
		o := modelsdialog.New(ctx, host.ModelsDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.modelsOverlay = &o
	case ui.DialogModelPick:
		host, ok := m.app.(modelPickDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Model picker is unavailable in this session.")
			return
		}
		o := modelpickdialog.New(ctx, host.ModelPickDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.modelPickOverlay = &o
	case ui.DialogModes:
		host, ok := m.app.(modesDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Modes dialog is unavailable in this session.")
			return
		}
		o := modesdialog.New(ctx, host.ModesDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.modesOverlay = &o
	case ui.DialogSystemPrompt:
		host, ok := m.app.(systemPromptDialogHost)
		if !ok {
			m.addBlock(roleInfo, "System prompt picker is unavailable in this session.")
			return
		}
		o := systempromptdialog.New(ctx, host.SystemPromptDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.systemPromptOverlay = &o
	case ui.DialogMCP:
		host, ok := m.app.(mcpDialogHost)
		if !ok {
			m.addBlock(roleInfo, "MCP server wizard is unavailable in this session.")
			return
		}
		o := mcpdialog.New(ctx, host.MCPDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.mcpOverlay = &o
	case ui.DialogTools:
		host, ok := m.app.(toolsDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Tool inventory is unavailable in this session.")
			return
		}
		o := toolsdialog.New(ctx, host.ToolsDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.toolsOverlay = &o
	}
}

func (m *model) View() string {
	if m.quitting {
		return ""
	}
	if m.onboardingOverlay != nil {
		return m.onboardingOverlay.View()
	}
	if m.overlay != nil {
		return m.overlay.View()
	}
	if m.modelsOverlay != nil {
		return m.modelsOverlay.View()
	}
	if m.modelPickOverlay != nil {
		return m.modelPickOverlay.View()
	}
	if m.modesOverlay != nil {
		return m.modesOverlay.View()
	}
	if m.systemPromptOverlay != nil {
		return m.systemPromptOverlay.View()
	}
	if m.mcpOverlay != nil {
		return m.mcpOverlay.View()
	}
	if m.toolsOverlay != nil {
		return m.toolsOverlay.View()
	}
	m.syncInputLayout()

	header := renderHeader(m.opts, m.th, m.width)
	footer := renderFooter(m.statusWithMetrics(), m.th, m.width)
	inputLine := m.input.View()
	suggestions := m.renderSuggestions()

	bodyHeight := m.bodyHeight()
	m.viewport.Height = bodyHeight
	m.viewport.Width = max(m.width-2, 1)

	sections := []string{header, m.viewport.View()}
	if band := m.renderConfirmBand(); band != "" {
		sections = append(sections, band)
	}
	if m.working {
		sections = append(sections, renderWorkingLine(m.spin, m.workingDisplayLabel(), m.th, m.width))
	}
	sections = append(sections, inputLine)
	if suggestions != "" {
		sections = append(sections, suggestions)
	}
	sections = append(sections, footer)
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// confirmChoices are the selectable answers in the confirmation band, ordered
// to match confirmChoice (0=once, 1=session, 2=no).
var confirmChoices = []string{"Allow once", "Allow for this session", "No"}

// confirmDiffMaxLines caps the diff preview height in the confirm band so a
// large write does not push the input off-screen.
const confirmDiffMaxLines = 20

// confirmDecisionForChoice maps a highlighted band row to its decision.
func confirmDecisionForChoice(choice int) ui.ConfirmDecision {
	switch choice {
	case 1:
		return ui.ConfirmSession
	case 2:
		return ui.ConfirmDeny
	default:
		return ui.ConfirmOnce
	}
}

// sendConfirm delivers the user's decision to the waiting scheduler and clears
// the confirmation state, resuming the working indicator.
func (m *model) sendConfirm(d ui.ConfirmDecision) {
	if m.confirmReply == nil {
		return
	}
	m.confirmReply <- d
	m.confirmReply = nil
	m.confirmText = ""
	m.confirmDiff = ""
	m.confirmChoice = 0
	m.status.Left = ""
	m.setWorking(true, "Thinking…")
}

// confirmBandLines builds the inner (un-bordered) lines of the confirmation
// panel: a title, an optional colorized diff preview, and the choice list with
// the current selection marked. Used by both renderConfirmBand and
// confirmBandRows so their heights stay in sync.
func (m *model) confirmBandLines() []string {
	title := m.confirmText
	if title == "" {
		title = "Run tool?"
	}
	lines := []string{m.th.Accent.Render("Confirm: ") + m.th.Primary.Render(title)}
	if m.confirmDiff != "" {
		lines = append(lines, m.renderDiffLines(m.confirmDiff, max(m.width-4, 1), confirmDiffMaxLines)...)
	}
	for i, c := range confirmChoices {
		row := fmt.Sprintf("%d %s", i+1, c)
		if i == m.confirmChoice {
			lines = append(lines, m.th.Selected.Render("› "+row))
		} else {
			lines = append(lines, "  "+m.th.Secondary.Render(row))
		}
	}
	return lines
}

// renderConfirmBand draws a focused panel above the input while a tool
// confirmation is pending, so the prompt is not lost in scrollback. It shows a
// diff preview (write_file) and a selectable Allow-once / Allow-for-session /
// No list.
func (m *model) renderConfirmBand() string {
	if m.confirmReply == nil {
		return ""
	}
	body := strings.Join(m.confirmBandLines(), "\n")
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(max(m.width-2, 1))
	if m.th.Colored {
		style = style.BorderForeground(m.th.FocusBorderColor)
	}
	return style.Render(body)
}

// suggestionWindow calculates the visible slice of suggestions, keeping the
// highlighted item in view. It returns the start index, number of items to show,
// and whether there are hidden items above or below the window.
func (m *model) suggestionWindow() (start, count int, showTop, showBottom bool) {
	n := len(m.suggestions)
	if n == 0 {
		return 0, 0, false, false
	}
	count = n
	if count > maxVisibleSuggestions {
		count = maxVisibleSuggestions
	}
	start = 0
	if m.suggestionIdx >= 0 {
		start = m.suggestionIdx - count + 1
		if start < 0 {
			start = 0
		}
	}
	showTop = start > 0
	showBottom = start+count < n
	return start, count, showTop, showBottom
}

// renderSuggestions draws the inline completion list, highlighting the selected
// row. It returns "" when there is nothing to show.
func (m *model) renderSuggestions() string {
	start, count, showTop, showBottom := m.suggestionWindow()
	if count == 0 {
		return ""
	}

	var b strings.Builder
	if showTop {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("  ↑ %d more", start)))
		b.WriteString("\n")
	}
	for i := start; i < start+count; i++ {
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
	if showBottom {
		b.WriteString(m.th.Dim.Render(fmt.Sprintf("  ↓ %d more", len(m.suggestions)-(start+count))))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *model) handleWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.viewport.Width = max(msg.Width-2, 1)
	m.viewport.Height = m.bodyHeight()
	m.syncInputLayout()
	m.syncViewportContent()
	return m, nil
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirmReply != nil {
		switch msg.String() {
		case "up":
			m.confirmChoice = (m.confirmChoice + 2) % 3
			return m, nil
		case "down":
			m.confirmChoice = (m.confirmChoice + 1) % 3
			return m, nil
		case "enter":
			m.sendConfirm(confirmDecisionForChoice(m.confirmChoice))
			return m, nil
		case "1", "y", "Y":
			m.sendConfirm(ui.ConfirmOnce)
			return m, nil
		case "2":
			m.sendConfirm(ui.ConfirmSession)
			return m, nil
		case "3", "n", "N", "esc":
			m.sendConfirm(ui.ConfirmDeny)
			return m, nil
		case "ctrl+c":
			return m, m.beginQuit()
		}
		return m, nil
	}

	if m.busy {
		switch msg.String() {
		case "esc":
			if m.turnCancel != nil {
				m.cancelTurn()
			} else if m.turnCanceled {
				// Second Esc: context already canceled; force-abandon so the
				// user is not stuck waiting for slow-to-drain tools.
				m.forceAbandonTurn()
			}
			return m, nil
		case "ctrl+c":
			// While a cancelable turn runs, Ctrl+C cancels it rather than
			// quitting outright, so a reflexive Ctrl+C does not end the session;
			// a second Ctrl+C (now idle) quits.
			if m.turnCancel != nil {
				m.cancelTurn()
				return m, nil
			}
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
	case "ctrl+/":
		if cycler, ok := m.app.(interface {
			CycleModel(context.Context) (<-chan ui.StreamEvent, error)
		}); ok {
			ctx := m.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			events, err := cycler.CycleModel(ctx)
			if err != nil {
				_ = m.term.ShowError(err)
				return m, nil
			}
			m.busy = true
			m.status.Left = "model"
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
	m.syncInputLayout()
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

// refreshSuggestions recomputes the completion list from the current input.
// Slash commands (line starts with "/") use the slash Completer; otherwise an
// active "@path" mention at the end of the input uses the MentionCompleter. It
// is a no-op when neither applies. Selection resets to "typing" (no highlight)
// on every change.
func (m *model) refreshSuggestions() {
	m.clearSuggestions()
	val := m.input.Value()
	if strings.HasPrefix(val, "/") {
		completer, ok := m.app.(ui.Completer)
		if !ok {
			return
		}
		res := completer.Complete(val)
		m.suggestions = res.Items
		m.completionFrom = res.ReplaceFrom
		return
	}
	if mc, ok := m.app.(ui.MentionCompleter); ok {
		res := mc.CompleteMention(val, m.inputByteCursor())
		m.suggestions = res.Items
		m.completionFrom = res.ReplaceFrom
	}
}

// inputByteCursor returns the byte offset of the textarea cursor within
// m.input.Value(), so @-mention completion targets the token the cursor is in
// rather than always the end of the input. The textarea exposes no flat offset,
// so the column is reconstructed from LineInfo (StartColumn+ColumnOffset is the
// cursor's rune column within the current logical line) and added to the byte
// length of the preceding lines.
func (m *model) inputByteCursor() int {
	val := m.input.Value()
	lines := strings.Split(val, "\n")
	row := m.input.Line()
	if row < 0 || row >= len(lines) {
		return len(val)
	}
	li := m.input.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	off := 0
	for i := 0; i < row; i++ {
		off += len(lines[i]) + 1 // +1 for the '\n' separator
	}
	runes := []rune(lines[row])
	if col < 0 {
		col = 0
	}
	if col > len(runes) {
		col = len(runes)
	}
	return off + len(string(runes[:col]))
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
	// Preserve any text after the cursor so completing a token mid-line does not
	// truncate the rest of the input. For the common end-of-input case cur ==
	// len(val) and this collapses to the previous behaviour.
	cur := m.inputByteCursor()
	if cur < from || cur > len(val) {
		cur = len(val)
	}
	insert := s.Insert
	if s.AppendSpace {
		insert += " "
	}
	m.input.SetValue(val[:from] + insert + val[cur:])
	m.input.CursorEnd()
	m.refreshSuggestions()
}

// cancelTurn aborts the in-flight turn (the stream will close with an error /
// done event) and notes it in the scrollback. The spinner keeps running with
// a "Canceling…" label until StreamDone drains or the user force-abandons.
func (m *model) cancelTurn() {
	if m.turnCancel == nil {
		return
	}
	m.turnCancel()
	m.turnCancel = nil
	m.turnCanceled = true
	m.setWorking(true, "Canceling…")
	m.addBlock(roleInfo, "Turn canceled.")
}

// forceAbandonTurn immediately restores the idle UI state when the user presses
// Esc a second time while waiting for a canceled turn to drain. The stream
// channel is detached and drained by a background goroutine so its writer
// goroutine can unblock without leaking.
func (m *model) forceAbandonTurn() {
	m.busy = false
	m.turnCanceled = false
	m.streamAbandoned = true
	m.turnStart = time.Time{}
	m.setWorking(false, "")
	m.status = m.idleStatus
	if ch := m.stream; ch != nil {
		m.stream = nil
		go func() {
			for ev := range ch {
				if ev.Type == ui.StreamDone || ev.Type == ui.StreamError {
					return
				}
			}
		}()
	}
}

// clearTurn releases the per-turn cancel function and resets the elapsed clock
// without emitting a cancellation notice (used on normal turn completion).
func (m *model) clearTurn() {
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.turnCanceled = false
	m.streamAbandoned = false
	m.turnStart = time.Time{}
}

// workingDisplayLabel augments the working/thinking label with an elapsed timer
// and a contextual hint: "esc to cancel" while running, "esc to force stop"
// while waiting for the canceled turn's goroutine to drain.
func (m *model) workingDisplayLabel() string {
	if m.turnStart.IsZero() {
		return m.workingLabel
	}
	secs := int(time.Since(m.turnStart).Seconds())
	hint := "esc to cancel"
	if m.turnCanceled {
		hint = "esc to force stop"
	}
	return fmt.Sprintf("%s (%ds · %s)", m.workingLabel, secs, hint)
}

func (m *model) handleSubmit(line string) (tea.Model, tea.Cmd) {
	if line == "" {
		return m, nil
	}

	m.addBlock(roleUser, line)
	m.busy = true
	m.streamAbandoned = false
	m.setWorking(true, "Thinking…")

	base := m.ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	m.turnCancel = cancel
	m.turnStart = time.Now()

	events, err := m.app.HandleInput(ctx, line)
	if err != nil {
		m.clearTurn()
		m.busy = false
		m.setWorking(false, "")
		m.status = m.idleStatus
		_ = m.term.ShowError(err)
		return m, nil
	}
	m.stream = events
	return m, tea.Batch(waitStream(events), m.spin.Tick)
}

func (m *model) handleStream(ev ui.StreamEvent) (tea.Model, tea.Cmd) {
	// If the stream was force-abandoned, discard stale events that were already
	// queued in the Bubble Tea message loop before the abandonment.
	if m.streamAbandoned {
		return m, nil
	}
	switch ev.Type {
	case ui.StreamTextDelta:
		// Hide the working line while the model is streaming visible output; the
		// response block itself signals activity. (Cheap path: no extra sync.)
		m.working = false
		m.addResponseDelta(ev.Text)
	case ui.StreamInfo:
		m.addBlock(roleInfo, ev.Text)
	case ui.StreamClearScrollback:
		m.clearScrollbackBlocks()
	case ui.StreamScrollback:
		m.addBlock(scrollbackRoleToRole(ev.ScrollbackRole), ev.Text)
	case ui.StreamCopyToClipboard:
		cmd := m.copyToClipboard(ev.Text)
		if m.stream != nil {
			return m, tea.Batch(cmd, waitStream(m.stream))
		}
		return m, cmd
	case ui.StreamSetTheme:
		m.setTheme(ev.Text)
	case ui.StreamQuit:
		m.busy = false
		m.setWorking(false, "")
		return m, m.beginQuit()
	case ui.StreamOpenDialog:
		m.openDialog(ev.Dialog)
	case ui.StreamToolStart:
		label := ev.ToolName
		if ev.Text != "" {
			label += " " + ev.Text
		}
		m.addBlock(roleToolStart, label)
		m.setWorking(true, "Running "+ev.ToolName)
	case ui.StreamToolConfirm:
		text := ev.ToolName
		if ev.Text != "" {
			text += ": " + ev.Text
		}
		m.addBlock(roleConfirm, text)
		m.confirmReply = ev.ConfirmReply
		m.confirmText = text
		m.confirmDiff = ev.Diff
		m.confirmChoice = 0
		// Awaiting the user; hide the spinner so the confirm band stands alone.
		m.setWorking(false, "")
		m.status.Left = "confirm tool"
	case ui.StreamToolResult:
		text := ev.ToolName
		if ev.Text != "" {
			text += " " + ev.Text
		}
		m.addBlock(roleToolResult, text)
		// Tool finished; the model will be queried again next.
		m.setWorking(true, "Thinking…")
	case ui.StreamError:
		if ev.Err != nil {
			m.addBlock(roleError, ev.Err.Error())
		} else if ev.Text != "" {
			m.addBlock(roleError, ev.Text)
		}
		m.setWorking(false, "")
	case ui.StreamDone:
		m.busy = false
		m.clearTurn()
		m.setWorking(false, "")
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
	m.syncInputPrompt(s.Mode)
}

// syncInputPrompt sets the input prefix to "<Mode> " (e.g. "Plan> ").
func (m *model) syncInputPrompt(mode string) {
	m.input.Prompt = inputPromptForMode(mode)
}

func inputPromptForMode(mode string) string {
	mode = strings.TrimSpace(strings.ToLower(mode))
	if mode == "" {
		mode = "agent"
	}
	return strings.ToUpper(mode[:1]) + mode[1:] + "> "
}

// addBlock appends a discrete (non-streaming) scrollback block and closes any
// open assistant response so the next text delta starts a fresh block.
// scrollbackRoleToRole maps a ui-level restored-block role onto the internal
// scrollRole used by the renderer.
func scrollbackRoleToRole(r ui.ScrollbackRole) scrollRole {
	switch r {
	case ui.ScrollbackUser:
		return roleUser
	case ui.ScrollbackAssistant:
		return roleResponse
	default:
		return roleInfo
	}
}

// copyToClipboard returns a command that copies text to the local clipboard off
// the UI goroutine. atotto shells out to xclip/wl-copy/pbcopy, which can block,
// so the copy runs inside the command and reports back via clipboardResultMsg
// rather than blocking Update.
func (m *model) copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		return clipboardResultMsg{text: text, err: clipboard.Copy(text)}
	}
}

// handleClipboardResult renders the outcome of an async clipboard copy, falling
// back to an OSC 52 escape sequence (emitted via tea.Printf, the only
// display-safe path) when no local clipboard mechanism is available.
func (m *model) handleClipboardResult(msg clipboardResultMsg) tea.Cmd {
	switch {
	case msg.err == nil:
		m.addBlock(roleInfo, "Copied last response to the clipboard.")
		return nil
	case errors.Is(msg.err, clipboard.ErrUnavailable):
		m.addBlock(roleInfo, "Copied last response via terminal clipboard (OSC 52).")
		return tea.Printf("%s", clipboard.OSC52Sequence(msg.text))
	default:
		m.addBlock(roleError, "Clipboard copy failed: "+msg.err.Error())
		return nil
	}
}

func (m *model) addBlock(role scrollRole, text string) {
	m.blocks = append(m.blocks, scrollBlock{role: role, text: strings.TrimRight(text, "\n")})
	m.openResponseIdx = -1
	m.syncViewportContent()
}

// clearScrollbackBlocks drops every scrollback block and closes any open
// assistant response so a restored conversation (/chat resume) replaces the
// visible history instead of being appended beneath it. The static welcome
// banner is preserved.
func (m *model) clearScrollbackBlocks() {
	m.blocks = nil
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

// setWorking toggles the working/thinking indicator and re-syncs the viewport so
// it stays scrolled to the bottom as the reserved row appears or disappears. The
// label is kept when turning the indicator on.
func (m *model) setWorking(on bool, label string) {
	m.working = on
	if on && label != "" {
		m.workingLabel = label
	}
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
	for i, blk := range m.blocks {
		// Separate turns visually: a blank line before each user prompt except
		// the first block, giving the conversation a readable rhythm.
		if blk.role == roleUser && i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.renderBlock(blk, width)...)
	}
	return strings.Join(lines, "\n")
}

// roleStyle returns the prefix glyph plus the prefix and body styles for a role.
func (m *model) roleStyle(role scrollRole) (glyph string, prefix, body lipgloss.Style) {
	switch role {
	case roleUser:
		return "You › ", m.th.Accent, m.th.UserBody
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
	switch {
	case blk.role == roleResponse:
		rendered = renderMarkdown(blk.text, max(width-gw, 1), m.th)
	case (blk.role == roleToolResult || blk.role == roleConfirm) && looksLikeDiff(blk.text):
		rendered = m.renderDiffLines(blk.text, max(width-gw, 1), diffResultMaxLines)
	default:
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

// diffResultMaxLines caps how many lines of a write_file result diff are shown
// in the scrollback before a "… N more" footer.
const diffResultMaxLines = 60

// looksLikeDiff reports whether text begins like a unified diff produced by
// internal/diff (a "--- a/" file marker or an "@@" hunk header).
func looksLikeDiff(text string) bool {
	return strings.HasPrefix(text, "--- ") || strings.HasPrefix(text, "@@")
}

// renderDiffLines colorizes unified-diff text: additions green, deletions red,
// hunk/file markers dim, context faint. width bounds wrapping; maxLines caps
// the output with a "… N more" footer (0 = no cap). Marker prefixes (@@/---/+++)
// are checked before the single +/- so they are not miscolored.
func (m *model) renderDiffLines(text string, width, maxLines int) []string {
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make([]string, 0, len(raw))
	truncated := 0
	for i, ln := range raw {
		if maxLines > 0 && len(out) >= maxLines {
			truncated = len(raw) - i
			break
		}
		var style lipgloss.Style
		switch {
		case strings.HasPrefix(ln, "@@"), strings.HasPrefix(ln, "+++"), strings.HasPrefix(ln, "---"):
			style = m.th.DiffMeta
		case strings.HasPrefix(ln, "+"):
			style = m.th.DiffAdd
		case strings.HasPrefix(ln, "-"):
			style = m.th.DiffDel
		default:
			style = m.th.Dim
		}
		for _, wl := range strings.Split(wrapText(ln, max(width, 1)), "\n") {
			out = append(out, style.Render(wl))
		}
	}
	if truncated > 0 {
		out = append(out, m.th.Dim.Render(fmt.Sprintf("… %d more diff lines", truncated)))
	}
	return out
}

// maxInputRows caps the visible height of the input box; longer content scrolls
// inside it.
const maxInputRows = 6

// wordWrapLineCount computes the number of soft-wrapped lines that a single line of
// text would occupy when word-wrapped to width.
func wordWrapLineCount(text string, width int) int {
	if width <= 0 {
		return 1
	}
	// Split into words, but keep whitespace/spaces
	var parts []string
	var current strings.Builder
	for _, r := range text {
		if r == ' ' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			parts = append(parts, " ")
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	lines := 1
	currentLen := 0
	for _, part := range parts {
		partLen := runewidth.StringWidth(part)
		if part == " " {
			if currentLen+1 <= width {
				currentLen++
			} else {
				lines++
				currentLen = 0
			}
		} else {
			if currentLen == 0 {
				if partLen > width {
					lines += (partLen - 1) / width
					currentLen = partLen % width
				} else {
					currentLen = partLen
				}
			} else if currentLen+partLen <= width {
				currentLen += partLen
			} else {
				lines++
				if partLen > width {
					lines += (partLen - 1) / width
					currentLen = partLen % width
				} else {
					currentLen = partLen
				}
			}
		}
	}
	return lines
}

// inputHeight is the number of terminal rows the input band occupies in the
// outer layout (1–maxInputRows).
func (m *model) inputHeight() int {
	if strings.TrimSpace(m.input.Value()) == "" {
		return 1
	}
	return inputBoxHeight(inputContentLines(m.input))
}

func (m *model) bodyHeight() int {
	// Baseline 6 covers header, a single input row, and the two-line footer;
	// add the extra input rows when the box has grown for a multi-line prompt.
	chrome := 6 + m.suggestionRows() + m.confirmBandRows() + m.workingRows() + (m.inputHeight() - 1)
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
}

// workingRows is the height of the working/thinking indicator line (1 while the
// agent is working, else 0) so the viewport shrinks to make room for it.
func (m *model) workingRows() int {
	if m.working {
		return 1
	}
	return 0
}

// confirmBandRows is the height of the confirm panel while a tool confirmation
// is pending, else 0. It mirrors renderConfirmBand: the inner lines plus the
// rounded border's top and bottom rows.
func (m *model) confirmBandRows() int {
	if m.confirmReply == nil {
		return 0
	}
	return len(m.confirmBandLines()) + 2
}

// suggestionRows is the number of terminal lines the suggestion block occupies,
// including the optional "↑/↓ N more" lines, so the viewport can shrink to fit.
func (m *model) suggestionRows() int {
	_, count, showTop, showBottom := m.suggestionWindow()
	rows := count
	if showTop {
		rows++
	}
	if showBottom {
		rows++
	}
	return rows
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

// statusWithMetrics augments the footer with live token usage and optional cost.
//
// Footer line 1 (Right): last-turn "↑{in} ↓{out}" + optional " ${cost}" when
// OpenRouter cost is known, + optional "  {pct}% context" when a limit exists.
// Footer line 2 (Detail): existing system-prompt preset label + "  Σ {in}/{out}"
// session total + optional " ${cost}" when known. The detail line is always shown
// even when no context limit is available (e.g. Gemini).
//
// On narrow terminals (< 80 cols) the session-total and cost parts are dropped to
// keep the footer readable.
func (m *model) statusWithMetrics() ui.StatusBar {
	status := m.status
	mp, ok := m.app.(ui.MetricsProvider)
	if !ok {
		return status
	}
	stats := mp.SessionMetrics()
	wide := m.width >= 80

	// ── Line 1: per-turn usage + optional context% ────────────────────────────
	var right string
	if stats.LastInputTokens > 0 || stats.LastOutputTokens > 0 {
		right = fmt.Sprintf("↑%s ↓%s",
			ui.CompactCount(stats.LastInputTokens),
			ui.CompactCount(stats.LastOutputTokens))
		if wide && stats.LastCostKnown {
			right += "  " + ui.FormatCostUSD(stats.LastCostUSD)
		}
	}
	if pct := stats.ContextPercent(); pct >= 0 {
		ctxStr := fmt.Sprintf("%d%% ctx", pct)
		if right != "" {
			right = right + "  ·  " + ctxStr
		} else {
			right = ctxStr
		}
	}
	if status.Right != "" && right != "" {
		status.Right = status.Right + "  ·  " + right
	} else if right != "" {
		status.Right = right
	}

	// ── Line 2: system-prompt label + session totals ──────────────────────────
	if wide && (stats.InputTokens > 0 || stats.OutputTokens > 0) {
		sessionStr := fmt.Sprintf("Σ %s/%s",
			ui.CompactCount(stats.InputTokens),
			ui.CompactCount(stats.OutputTokens))
		if stats.SessionCostKnown {
			sessionStr += "  " + ui.FormatCostUSD(stats.SessionCostUSD)
		}
		if status.Detail != "" {
			status.Detail = status.Detail + "  ·  " + sessionStr
		} else {
			status.Detail = sessionStr
		}
	}

	return status
}

func renderFooter(status ui.StatusBar, th theme.Theme, width int) string {
	left := status.Left
	right := status.Right

	// Measure raw string widths before injecting ANSI so the gap is accurate.
	gap := max(width-lipgloss.Width(left)-lipgloss.Width(right), 1)

	leftRendered := th.Secondary.Render(left)
	var rightRendered string
	if len(th.TitleGradient) > 0 && right != "" {
		rightRendered = th.GradientText(right, th.Secondary, th.TitleGradient)
	} else {
		rightRendered = th.Secondary.Render(right)
	}

	line := leftRendered + strings.Repeat(" ", gap) + rightRendered

	if status.Detail == "" {
		return line
	}

	var detail string
	if len(th.TitleGradient) > 0 {
		detail = th.GradientText(status.Detail, th.Secondary, th.TitleGradient)
	} else {
		detail = th.Secondary.Render(status.Detail)
	}
	return line + "\n" + detail
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
