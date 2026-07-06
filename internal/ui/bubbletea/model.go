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
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/bgprocdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelpickdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modesdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/onboardingdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/settingsdialog"
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

// settingsDialogHost is implemented by an App that can supply the curated
// settings browser dependencies.
type settingsDialogHost interface {
	SettingsDialogDeps() settingsdialog.Deps
}

// onboardingHost is implemented by an App that can supply first-run setup deps.
type onboardingHost interface {
	OnboardingDialogDeps() onboardingdialog.Deps
}

type streamEventMsg struct {
	gen   uint64
	event ui.StreamEvent
}

// turnDrainDoneMsg signals that a force-abandoned stream channel has fully
// drained so a new turn may start on the shared Runner.
type turnDrainDoneMsg struct{}

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
	// roleToolCard is a grouped tool invocation rendered as a bordered card.
	// The block carries a *toolCard whose phase/body update in place across the
	// invocation's lifecycle.
	roleToolCard
)

// scrollBlock is one logical message in the scrollback. text may contain
// embedded newlines; the renderer wraps and prefixes it at paint time. For
// roleToolCard blocks the content lives on card instead of text.
type scrollBlock struct {
	role scrollRole
	text string
	card *toolCard
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
	// lastTextDeltaAt is when the most recent assistant text delta arrived. The
	// working spinner is suppressed only briefly after it (streamingSpinnerGrace)
	// so the spinner reappears during the silent wait for the model's next action
	// even though the response block stays open.
	lastTextDeltaAt time.Time

	// Tool cards: activeCard is the invocation currently executing or awaiting
	// confirmation (tool events arrive serially from the scheduler); cardByID
	// indexes every card by its call id so out-of-order or interleaved events
	// still update the right card.
	activeCard *toolCard
	cardByID   map[string]*toolCard

	// followBottom pins the scrollback viewport to the newest content. It is
	// true until the user scrolls up (PgUp/wheel/Shift+Up) and is restored when
	// they scroll back to the bottom or submit a new turn, so incoming output
	// does not yank the view away while the user is reading history.
	followBottom bool

	// mouseEnabled tracks whether mouse-wheel reporting is currently active.
	// Mouse capture is OFF at launch so the terminal's native text selection
	// works; Alt+M (or the /mouse command) toggles it via tea.EnableMouseCellMotion
	// / tea.DisableMouse at runtime.
	mouseEnabled bool

	busy     bool
	quitting bool
	// turnCancel cancels the in-flight HandleInput turn; nil when no cancelable
	// turn is running. turnStart marks when the turn began, for the elapsed
	// timer in the working line. turnCanceled is true from the first Esc press
	// until StreamDone arrives or the stream is force-abandoned.
	// turnInFlight stays true from a successful submit until StreamDone or the
	// abandoned stream channel drains, so HandleInput cannot overlap on Runner.
	// streamGen/activeStreamGen tag streamEventMsg values so stale events queued
	// before force-abandon cannot apply to a later turn.
	turnCancel      context.CancelFunc
	turnStart       time.Time
	turnCanceled    bool
	turnInFlight    bool
	streamGen       uint64
	activeStreamGen uint64
	// spin drives the animated working/thinking indicator shown above the input
	// while a turn is in flight. It only ticks while busy.
	spin spinner.Model
	// working toggles visibility of the working indicator line; workingLabel is
	// the current activity (e.g. "Working…" or "Running Shell").
	working      bool
	workingLabel string
	runningTool  string

	// Thinking ("reasoning") box state. thinking accumulates the reasoning text
	// streamed for the current turn (cleared on StreamDone). showThinking is the
	// live session visibility; thinkingToggled records whether the user has
	// overridden the resolved per-model/global setting via Ctrl+T.
	thinking        string
	showThinking    bool
	thinkingToggled bool
	// exitSummary is captured when quitting begins so the Terminal can print the
	// goodbye screen after the alt-screen program tears down.
	exitSummary string
	stream      <-chan ui.StreamEvent
	// confirmReply is set while a tool confirmation is pending; the active tool
	// card renders the numbered menu in scrollback until the user picks a
	// choice. The decision is once / session / deny.
	confirmReply chan ui.ConfirmDecision
	// confirmChoice is the highlighted option in the confirming card's menu
	// (0=once, 1=session, 2=no).
	confirmChoice int

	// Slash-command autocompletion state.
	suggestions    []ui.Suggestion
	suggestionIdx  int // -1 means nothing highlighted (user is still typing)
	completionFrom int // byte offset in the input where the active token starts

	// history is the prompt-history navigator (Up/Down/Ctrl+P/Ctrl+N at the
	// input boundaries). queue holds messages typed while a turn is in flight;
	// they are submitted in order once the turn completes.
	history *inputHistory
	queue   []string

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
	// bgProcOverlay holds the background process viewer.
	bgProcOverlay *bgprocdialog.Model
	// settingsOverlay holds the curated settings browser.
	settingsOverlay *settingsdialog.Model
	// onboardingOverlay holds the first-run provider setup wizard.
	onboardingOverlay *onboardingdialog.Model
}

// hasOverlay reports whether any modal dialog is currently active.
func (m *model) hasOverlay() bool {
	return m.onboardingOverlay != nil || m.overlay != nil ||
		m.modelsOverlay != nil || m.modelPickOverlay != nil || m.modesOverlay != nil ||
		m.systemPromptOverlay != nil || m.mcpOverlay != nil || m.toolsOverlay != nil ||
		m.bgProcOverlay != nil || m.settingsOverlay != nil
}

// maxVisibleSuggestions caps the inline suggestion list height.
const maxVisibleSuggestions = 8

func newModel(opts ui.Options, app ui.App, term *Terminal) *model {
	ti := textarea.New()
	ti.Placeholder = inputPlaceholderIdle
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
		opts:              opts,
		app:               app,
		term:              term,
		th:                th,
		welcome:         welcome,
		openResponseIdx: -1,
		cardByID:        make(map[string]*toolCard),
		input:           ti,
		viewport:        vp,
		spin:            newWorkingSpinner(th),
		status:          idleStatus,
		idleStatus:      idleStatus,
		suggestionIdx:   -1,
		history:         newInputHistory(),
		followBottom:    true,
		showThinking:    opts.ShowThinking,
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
	case tea.MouseMsg:
		// Mouse-wheel scrolling of the conversation. The viewport only acts on
		// wheel-press events; forward and track whether we are still pinned to
		// the bottom so streamed output does not yank the view away.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.followBottom = m.viewport.AtBottom()
		return m, cmd
	case tea.KeyMsg:
		return m.handleKey(msg)
	case submitMsg:
		return m.handleSubmit(msg.line)
	case concurrentStreamEventMsg:
		if msg.gen != m.activeStreamGen {
			return m, nil
		}
		m, cmd := m.handleStream(msg.event)
		return m, tea.Batch(cmd, waitConcurrentStream(msg.ch, msg.gen))
	case streamEventMsg:
		return m.handleStreamGen(msg.gen, msg.event)
	case turnDrainDoneMsg:
		m.turnInFlight = false
		return m, nil
	case clipboardResultMsg:
		return m, m.handleClipboardResult(msg)
	case thinkingSavedMsg:
		if msg.err != nil {
			_ = m.term.ShowError(msg.err)
		}
		return m, nil
	case themeCycledMsg:
		if msg.err != nil {
			_ = m.term.ShowError(msg.err)
			return m, nil
		}
		m.addBlock(roleInfo, "Theme → "+msg.name+" (Alt+T).")
		m.setTheme(msg.name)
		return m, nil
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
		// A running tool card embeds the spinner in its header (inside the
		// viewport), so re-render the scrollback each tick to animate it.
		if m.activeCard != nil && m.activeCard.phase == toolRunning {
			m.syncViewportContent()
		}
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
		if m.bgProcOverlay != nil {
			next, _ := m.bgProcOverlay.Update(msg)
			m.bgProcOverlay = next
		}
		if m.settingsOverlay != nil {
			o := m.settingsOverlay.SetSize(msg.Width, msg.Height)
			m.settingsOverlay = &o
		}
		return m, nil
	case streamEventMsg:
		return m.handleStreamGen(msg.gen, msg.event)
	case turnDrainDoneMsg:
		m.turnInFlight = false
		return m, nil
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

	if m.bgProcOverlay != nil {
		next, cmd := m.bgProcOverlay.Update(msg)
		if _, ok := msg.(bgprocdialog.MsgDone); ok {
			m.closeOverlay("")
			return m, cmd
		}
		m.bgProcOverlay = next
		return m, cmd
	}

	if m.settingsOverlay != nil {
		next, cmd := m.settingsOverlay.Update(msg)
		if next.Done() {
			m.closeOverlay(next.Status())
			return m, cmd
		}
		m.settingsOverlay = &next
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
	m.bgProcOverlay = nil
	m.settingsOverlay = nil
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
	case ui.DialogSettings:
		host, ok := m.app.(settingsDialogHost)
		if !ok {
			m.addBlock(roleInfo, "Settings browser is unavailable in this session.")
			return
		}
		o := settingsdialog.New(ctx, host.SettingsDialogDeps())
		o = o.SetTheme(m.th)
		o = o.SetSize(m.width, m.height)
		m.settingsOverlay = &o
	case ui.DialogBackground:
		host, ok := m.app.(interface {
			ListBackgroundProcesses() []bgproc.Process
			KillBackgroundProcess(pid int) error
			BackgroundProcessOutput(pid int) string
		})
		if !ok {
			m.addBlock(roleInfo, "Background processes are unavailable in this session.")
			return
		}
		procs := host.ListBackgroundProcesses()
		o := bgprocdialog.New(m.width, m.height, m.th, procs, host.BackgroundProcessOutput, host.KillBackgroundProcess)
		m.bgProcOverlay = o
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
	if m.bgProcOverlay != nil {
		return m.bgProcOverlay.View()
	}
	if m.settingsOverlay != nil {
		return m.settingsOverlay.View()
	}
	m.syncInputLayout()
	m.syncInputPlaceholder()

	header := renderHeader(m.opts, m.th, m.width)
	footer := renderFooter(m.statusWithMetrics(), m.th, m.width)
	inputLine := m.input.View()
	suggestions := m.renderSuggestions()

	bodyHeight := m.bodyHeight()
	m.viewport.Height = bodyHeight
	m.viewport.Width = max(m.width-2, 1)

	sections := []string{header, m.viewport.View()}
	if row := m.renderStatusRow(); row != "" {
		sections = append(sections, row)
	}
	// The thinking box carries its own spinner in the border, so when it is
	// visible it replaces the standalone working line.
	if box := m.renderThinkingBox(); box != "" {
		sections = append(sections, box)
	} else if m.showWorkingIndicator() {
		sections = append(sections, renderWorkingLine(m.spin, m.workingDisplayLabel(), m.th, m.width))
	}
	sections = append(sections, m.renderInputSeparator())
	sections = append(sections, inputLine)
	if suggestions != "" {
		sections = append(sections, suggestions)
	}
	sections = append(sections, footer)
	return clampWidth(lipgloss.JoinVertical(lipgloss.Left, sections...), m.width)
}

// clampWidth truncates every line of the rendered frame to width cells. The
// composer layout is built so each section fits on a single (or fixed number of)
// row(s) at exactly width; any line that nonetheless exceeds the terminal width
// would soft-wrap and desynchronize Bubble Tea's frame diff, leaving ghost rows
// (duplicated status row / footer). This is a cheap, ANSI-aware safety net.
func clampWidth(frame string, width int) string {
	if width < 1 {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = ansi.Truncate(line, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// confirmChoices are the selectable answers in a confirming tool card, ordered
// to match confirmChoice (0=once, 1=session, 2=no).
var confirmChoices = []string{"Allow once", "Allow for this session", "No"}

// confirmDiffMaxLines caps the diff preview height in a confirming card so a
// large write does not push the input off-screen.
const confirmDiffMaxLines = 20

// confirmDecisionForChoice maps a highlighted menu row to its decision.
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

// sendConfirm delivers the user's decision to the waiting scheduler, returns the
// confirming card to its running phase, and resumes the working indicator.
func (m *model) sendConfirm(d ui.ConfirmDecision) {
	if m.confirmReply == nil {
		return
	}
	m.confirmReply <- d
	m.confirmReply = nil
	m.confirmChoice = 0
	m.status.Left = ""
	if m.activeCard != nil && m.activeCard.phase == toolConfirming {
		// Denials surface as an error result from the scheduler; for an approval
		// the card resumes running until output/result arrive.
		m.activeCard.phase = toolRunning
		m.activeCard.body = ""
		m.activeCard.diff = ""
	}
	m.setWorking(true, m.busyLabel())
	m.syncViewportContent()
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
	// Conversation scrolling works in every state (idle, busy, confirming) and
	// uses dedicated keys that do not collide with the input cursor or history.
	if m.handleScrollKey(msg.String()) {
		return m, nil
	}

	// Ctrl+T toggles the thinking box live in any state and persists the choice.
	if msg.String() == "ctrl+t" {
		return m.toggleThinking()
	}

	// Alt+M toggles mouse-wheel scrolling in any state. It is off by default so
	// the terminal's native click-drag text selection works. 'µ' is Mac Option+M.
	if msg.String() == "alt+m" || msg.String() == "µ" {
		return m, m.setMouse(!m.mouseEnabled)
	}

	// Alt+T cycles the color theme live in any state and persists the choice.
	// '†' is Mac Option+T.
	if msg.String() == "alt+t" || msg.String() == "†" {
		return m.cycleTheme()
	}

	if m.confirmReply != nil {
		switch msg.String() {
		case "up":
			m.confirmChoice = (m.confirmChoice + 2) % 3
			m.syncViewportContent()
			return m, nil
		case "down":
			m.confirmChoice = (m.confirmChoice + 1) % 3
			m.syncViewportContent()
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
				return m, m.forceAbandonTurn()
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
		case "enter":
			// Enter while a turn is running queues the message for submission
			// once the turn completes (gemini-cli parity), rather than blocking.
			return m.handleBusyEnter()
		case "tab":
			if len(m.suggestions) > 0 {
				idx := m.suggestionIdx
				if idx < 0 {
					idx = 0
				}
				m.acceptSuggestion(idx)
				return m, nil
			}
			return m.handleBusyTab()
		case "ctrl+shift+m", "ctrl+/":
			// Mode/model switching is not allowed mid-turn; ignore so the keys
			// do not corrupt the in-flight stream.
			return m, nil
		case "ctrl+b":
			m.openDialog(ui.DialogBackground)
			return m, nil
		case "up":
			if len(m.suggestions) > 0 {
				m.moveSuggestion(-1)
				return m, nil
			}
			return m.handleHistoryUp()
		case "down":
			if len(m.suggestions) > 0 {
				m.moveSuggestion(1)
				return m, nil
			}
			return m.handleHistoryDown()
		}
		// Otherwise keep the input editable while the turn runs so the user can
		// compose the next message; the cursor and typing stay responsive.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.syncInputLayout()
		m.refreshSuggestions()
		return m, cmd
	}

	switch msg.String() {
	case "ctrl+c":
		return m, m.beginQuit()
	case "ctrl+shift+m":
		if cycler, ok := m.app.(interface {
			CycleInteractionMode(context.Context) (<-chan ui.StreamEvent, error)
		}); ok {
			events, err := cycler.CycleInteractionMode(m.streamContext())
			return m.startAppStream(events, err, "mode")
		}
		return m, nil
	case "alt+1", "¡":
		return m.startModeSwitch("agent")
	case "alt+2", "™":
		return m.startModeSwitch("plan")
	case "alt+3", "£":
		return m.startModeSwitch("ask")
	case "alt+4", "¢":
		return m.startModeSwitch("debug")
	case "ctrl+/":
		if cycler, ok := m.app.(interface {
			CycleModel(context.Context) (<-chan ui.StreamEvent, error)
		}); ok {
			events, err := cycler.CycleModel(m.streamContext())
			return m.startAppStream(events, err, "model")
		}
		return m, nil
	case "ctrl+shift+p":
		if cycler, ok := m.app.(interface {
			CycleModelReverse(context.Context) (<-chan ui.StreamEvent, error)
		}); ok {
			events, err := cycler.CycleModelReverse(m.streamContext())
			return m.startAppStream(events, err, "model")
		}
		return m, nil
	case "ctrl+b":
		m.openDialog(ui.DialogBackground)
		return m, nil
	case "up":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(-1)
			return m, nil
		}
		return m.handleHistoryUp()
	case "down":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(1)
			return m, nil
		}
		return m.handleHistoryDown()
	case "ctrl+p":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(-1)
			return m, nil
		}
		if text, ok := m.history.up(m.input.Value()); ok {
			m.applyHistoryEntry(text, cursorStart)
		}
		return m, nil
	case "ctrl+n":
		if len(m.suggestions) > 0 {
			m.moveSuggestion(1)
			return m, nil
		}
		if text, ok := m.history.down(m.input.Value()); ok {
			m.applyHistoryEntry(text, cursorEnd)
		}
		return m, nil
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

// handleBusyEnter queues the current input for submission after the in-flight
// turn finishes. Most slash commands cannot be queued (they may depend on live
// session state), so they are rejected with a brief notice. However, specific
// safe slash commands (like /goal pause) can be executed concurrently.
func (m *model) handleBusyEnter() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())
	if line == "" {
		return m, nil
	}
	if strings.HasPrefix(line, "/") {
		// Allow safe read-only or goal-pausing commands to run concurrently.
		if isConcurrentSafeSlash(line) {
			m.input.SetValue("")
			m.clearSuggestions()
			m.syncInputLayout()
			return m, m.runConcurrentSlash(line)
		}
		m.addBlock(roleInfo, "Slash commands cannot be queued; wait for the current turn to finish.")
		return m, nil
	}
	m.enqueueMessage(line)
	m.input.SetValue("")
	m.clearSuggestions()
	m.syncInputLayout()
	return m, nil
}

func isConcurrentSafeSlash(line string) bool {
	l := strings.ToLower(line)
	return l == "/goal pause" || l == "/goal status" || l == "/stats" ||
		strings.HasPrefix(l, "/stats ")
}

func (m *model) runConcurrentSlash(line string) tea.Cmd {
	gen := m.activeStreamGen
	return func() tea.Msg {
		// Run in background context so it doesn't get canceled if the turn cancels.
		events, err := m.app.HandleInput(context.Background(), line)
		if err != nil {
			return streamEventMsg{gen: gen, event: ui.StreamEvent{Type: ui.StreamError, Err: err}}
		}
		// Return the first event, ignoring StreamDone to avoid ending the busy turn.
		for ev := range events {
			if ev.Type == ui.StreamDone {
				continue
			}
			return concurrentStreamEventMsg{gen: gen, event: ev, ch: events}
		}
		return nil
	}
}

// handleBusyTab queues the current (non-slash) input while a turn runs, matching
// gemini-cli's explicit Tab-to-queue. With no buffered text it is a no-op.
func (m *model) handleBusyTab() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())
	if line == "" || strings.HasPrefix(line, "/") {
		return m, nil
	}
	m.enqueueMessage(line)
	m.input.SetValue("")
	m.clearSuggestions()
	m.syncInputLayout()
	return m, nil
}

// enqueueMessage appends a message to the pending queue and records it in the
// prompt history so Up recalls it like a submitted prompt.
func (m *model) enqueueMessage(line string) {
	m.queue = append(m.queue, line)
	m.history.record(line)
	m.addBlock(roleInfo, fmt.Sprintf("Queued message (%d pending).", len(m.queue)))
}

// popQueuedIntoInput loads the entire pending queue into the input box when the
// input is empty (gemini-cli's "Up on empty input pops queued messages"). It
// returns true when it consumed the queue.
func (m *model) popQueuedIntoInput() bool {
	if len(m.queue) == 0 || strings.TrimSpace(m.input.Value()) != "" {
		return false
	}
	combined := strings.Join(m.queue, "\n\n")
	m.queue = nil
	m.input.SetValue(combined)
	m.syncInputLayout()
	m.refreshSuggestions()
	return true
}

// flushQueue submits any messages queued during the just-finished turn, joined
// with blank lines, as a single new turn. Returns the command that starts the
// stream, or nil when the queue is empty.
func (m *model) flushQueue() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	combined := strings.Join(m.queue, "\n\n")
	m.queue = nil
	return func() tea.Msg { return submitMsg{line: combined} }
}

// cursorPos selects where the cursor lands after a history entry is loaded.
type cursorPos int

const (
	cursorStart cursorPos = iota
	cursorEnd
)

// inputSingleVisualLine reports whether the input occupies a single visual
// (wrapped) row, so Up/Down should fall through to history navigation.
func (m *model) inputSingleVisualLine() bool {
	return m.input.LineCount() == 1 && m.input.LineInfo().Height == 1
}

// inputAtFirstVisualRow reports whether the cursor is on the first visual row of
// the whole input (logical line 0, first wrapped row).
func (m *model) inputAtFirstVisualRow() bool {
	return m.input.Line() == 0 && m.input.LineInfo().RowOffset == 0
}

// inputAtLastVisualRow reports whether the cursor is on the last visual row of
// the whole input (last logical line, last wrapped row).
func (m *model) inputAtLastVisualRow() bool {
	li := m.input.LineInfo()
	return m.input.Line() == m.input.LineCount()-1 && li.RowOffset == li.Height-1
}

// handleHistoryUp implements gemini-cli's Up behavior: move the cursor up a
// visual line within multi-line text; at the first row move to line start; when
// already at the start, pop any queued messages or step back through history.
func (m *model) handleHistoryUp() (tea.Model, tea.Cmd) {
	if !(m.inputSingleVisualLine() || m.inputAtFirstVisualRow()) {
		m.input.CursorUp()
		m.syncInputLayout()
		return m, nil
	}
	if m.input.LineInfo().ColumnOffset > 0 {
		m.input.CursorStart()
		m.syncInputLayout()
		return m, nil
	}
	if m.popQueuedIntoInput() {
		return m, nil
	}
	if text, ok := m.history.up(m.input.Value()); ok {
		m.applyHistoryEntry(text, cursorStart)
	}
	return m, nil
}

// handleHistoryDown implements gemini-cli's Down behavior: move the cursor down
// a visual line within multi-line text; at the last row move to line end; when
// already at the end, step forward through history (and finally the draft).
func (m *model) handleHistoryDown() (tea.Model, tea.Cmd) {
	if !(m.inputSingleVisualLine() || m.inputAtLastVisualRow()) {
		m.input.CursorDown()
		m.syncInputLayout()
		return m, nil
	}
	// Move to the end of the line first; if that changes the cursor we were not
	// at the end yet, so stop here. (LineInfo.Width can include a trailing
	// soft-wrap space, so compare the cursor column before/after instead.)
	before := m.input.LineInfo().ColumnOffset
	m.input.CursorEnd()
	if m.input.LineInfo().ColumnOffset != before {
		m.syncInputLayout()
		return m, nil
	}
	if text, ok := m.history.down(m.input.Value()); ok {
		m.applyHistoryEntry(text, cursorEnd)
	}
	return m, nil
}

// applyHistoryEntry replaces the input with a history entry and positions the
// cursor at the start (Up) or end (Down), matching gemini-cli's default cursor
// placement when browsing history.
func (m *model) applyHistoryEntry(text string, pos cursorPos) {
	m.input.SetValue(text)
	if pos == cursorStart {
		m.inputCursorToBegin()
	}
	m.syncInputLayout()
	m.refreshSuggestions()
}

// inputCursorToBegin moves the textarea cursor to the very beginning (row 0,
// col 0) using only the widget's public API. SetValue leaves the cursor at the
// end, so we walk up visual rows until reaching the top.
func (m *model) inputCursorToBegin() {
	for n := 0; n < 2000; n++ {
		li := m.input.LineInfo()
		if m.input.Line() == 0 && li.RowOffset == 0 {
			break
		}
		m.input.CursorUp()
	}
	m.input.CursorStart()
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
// channel is detached and drained asynchronously so its writer goroutine can
// unblock without leaking; turnInFlight stays true until that drain completes.
func (m *model) forceAbandonTurn() tea.Cmd {
	m.busy = false
	m.runningTool = ""
	m.turnCanceled = false
	m.turnStart = time.Time{}
	m.setWorking(false, "")
	m.status = m.idleStatus
	// Invalidate any streamEventMsg already queued in the Bubble Tea loop.
	m.activeStreamGen++
	if len(m.queue) > 0 {
		m.queue = nil
		m.addBlock(roleInfo, "Queued messages discarded after force stop.")
	}
	ch := m.stream
	m.stream = nil
	if ch == nil {
		m.turnInFlight = false
		return nil
	}
	return drainStreamCmd(ch)
}

// clearTurn releases the per-turn cancel function and resets the elapsed clock
// without emitting a cancellation notice (used on normal turn completion).
func (m *model) clearTurn() {
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.turnCanceled = false
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
	if m.turnInFlight {
		m.addBlock(roleInfo, "Wait for the previous turn to finish before submitting.")
		return m, nil
	}

	m.history.record(line)
	m.followBottom = true
	m.addBlock(roleUser, line)
	m.busy = true
	m.turnInFlight = true
	m.streamGen++
	m.activeStreamGen = m.streamGen
	m.setWorking(true, m.busyLabel())

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
		m.turnInFlight = false
		m.setWorking(false, "")
		m.status = m.idleStatus
		_ = m.term.ShowError(err)
		return m, nil
	}
	m.stream = events
	gen := m.activeStreamGen
	return m, tea.Batch(waitStream(events, gen), m.spin.Tick)
}

// handleStream applies ev to the current turn. Tests call this directly; live
// events arrive via handleStreamGen with an explicit generation tag.
func (m *model) handleStream(ev ui.StreamEvent) (tea.Model, tea.Cmd) {
	return m.handleStreamGen(m.activeStreamGen, ev)
}

func (m *model) handleStreamGen(gen uint64, ev ui.StreamEvent) (tea.Model, tea.Cmd) {
	if gen != m.activeStreamGen {
		return m, nil
	}
	switch ev.Type {
	case ui.StreamTextDelta:
		// Response text streams in the scrollback; keep the working indicator
		// visible whenever busy so gaps before tool calls still show activity.
		m.addResponseDelta(ev.Text)
	case ui.StreamReasoningDelta:
		// Reasoning ("thinking") streams into the dedicated box, never the
		// scrollback. Accumulate even when hidden so toggling on mid-turn shows
		// the thoughts so far. The buffer is cleared when the model stops
		// reasoning for a step — at the first tool start or answer-text delta
		// (see startToolCard/addResponseDelta) — and again at StreamDone.
		m.thinking += ev.Text
	case ui.StreamInfo:
		m.addBlock(roleInfo, ev.Text)
	case ui.StreamClearScrollback:
		m.clearScrollbackBlocks()
	case ui.StreamScrollback:
		m.addBlock(scrollbackRoleToRole(ev.ScrollbackRole), ev.Text)
	case ui.StreamCopyToClipboard:
		cmd := m.copyToClipboard(ev.Text)
		if m.stream != nil {
			return m, tea.Batch(cmd, waitStream(m.stream, gen))
		}
		return m, cmd
	case ui.StreamSetTheme:
		m.setTheme(ev.Text)
	case ui.StreamSetMouse:
		cmd := m.applyMouseMode(ev.Text)
		if m.stream != nil {
			return m, tea.Batch(cmd, waitStream(m.stream, gen))
		}
		return m, cmd
	case ui.StreamQuit:
		m.busy = false
		m.setWorking(false, "")
		return m, m.beginQuit()
	case ui.StreamOpenDialog:
		m.openDialog(ev.Dialog)
	case ui.StreamToolStart:
		m.startToolCard(ev)
		m.runningTool = ev.ToolName
		m.setWorking(true, m.busyLabel())
	case ui.StreamToolOutput:
		if c := m.toolCardFor(ev); c != nil {
			c.body = ev.Text
			m.syncViewportContent()
		}
	case ui.StreamToolConfirm:
		if c := m.toolCardFor(ev); c != nil {
			c.phase = toolConfirming
			c.diff = ev.Diff
			c.body = ev.Text
		}
		m.confirmReply = ev.ConfirmReply
		m.confirmChoice = 0
		// Keep the confirming card pinned in view; the spinner gives way to the
		// card's own '?' icon and inline menu.
		m.setWorking(false, "")
		m.status.Left = "confirm tool"
		m.followBottom = true
		m.syncViewportContent()
	case ui.StreamToolResult:
		if c := m.toolCardFor(ev); c != nil {
			c.body = ev.Text
			c.exitCode = ev.ExitCode
			if ev.IsError {
				c.phase = toolError
			} else {
				c.phase = toolSuccess
			}
		}
		m.activeCard = nil
		m.runningTool = ""
		m.syncViewportContent()
		// Tool finished; the model will be queried again next.
		m.setWorking(true, m.busyLabel())
	case ui.StreamError:
		if ev.Err != nil {
			m.addBlock(roleError, ev.Err.Error())
		} else if ev.Text != "" {
			m.addBlock(roleError, ev.Text)
		}
		m.activeCard = nil
		m.runningTool = ""
		m.setWorking(false, "")
	case ui.StreamDone:
		m.busy = false
		m.turnInFlight = false
		m.activeCard = nil
		m.runningTool = ""
		m.thinking = ""
		m.clearTurn()
		m.setWorking(false, "")
		m.closeResponse()
		m.refreshIdleStatus()
		m.status = m.idleStatus
		m.stream = nil
		// Submit any messages the user queued while this turn was running.
		if cmd := m.flushQueue(); cmd != nil {
			return m, cmd
		}
		return m, nil
	}
	if m.stream != nil {
		return m, waitStream(m.stream, gen)
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

// syncInputPrompt sets the input prefix to "<Mode> " (e.g. "Plan> "). It uses a
// per-line prompt function so only the first visual row carries the mode prefix;
// wrapped continuation rows (and the textarea's end-of-buffer padding rows) are
// blank, aligned under the text. This mirrors gemini-cli's single-prompt input
// and prevents a duplicate "Agent> " row from appearing below the cursor.
// Input placeholder strings. While a turn is in flight, anything submitted is
// queued for after the turn (handleBusyEnter), so the busy hint says "Queue"
// to signal the composer is not idle — the only other "still working" cue is
// the Working… spinner row.
const (
	inputPlaceholderIdle = "Type a message"
	inputPlaceholderBusy = "Queue a message"
)

// syncInputPlaceholder keeps the composer placeholder in step with the turn
// state so an editable input during a running turn does not look idle.
func (m *model) syncInputPlaceholder() {
	if m.busy {
		m.input.Placeholder = inputPlaceholderBusy
		return
	}
	m.input.Placeholder = inputPlaceholderIdle
}

func (m *model) syncInputPrompt(mode string) {
	prompt := inputPromptForMode(mode)
	width := runewidth.StringWidth(prompt)
	m.input.SetPromptFunc(width, func(line int) string {
		if line == 0 {
			return prompt
		}
		return ""
	})
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

// startToolCard appends a new running tool card for a StreamToolStart event,
// indexing it by call id and marking it active so subsequent output/confirm/
// result events update it in place.
func (m *model) startToolCard(ev ui.StreamEvent) {
	// The model has stopped reasoning for this step and is now acting, so retire
	// the Thinking box (its buffer is never replayed). A later step that reasons
	// again repopulates it; otherwise the tool card owns the spinner.
	m.thinking = ""
	card := newToolCard(ev)
	m.blocks = append(m.blocks, scrollBlock{role: roleToolCard, card: card})
	m.openResponseIdx = -1
	m.activeCard = card
	if card.callID != "" {
		if m.cardByID == nil {
			m.cardByID = make(map[string]*toolCard)
		}
		m.cardByID[card.callID] = card
	}
	m.syncViewportContent()
}

// toolCardFor resolves the card a follow-up tool event applies to: by call id
// when present, falling back to the active card (tool events arrive serially).
func (m *model) toolCardFor(ev ui.StreamEvent) *toolCard {
	if ev.ToolCallID != "" {
		if c, ok := m.cardByID[ev.ToolCallID]; ok {
			return c
		}
	}
	return m.activeCard
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
	m.lastTextDeltaAt = time.Now()
	if m.openResponseIdx < 0 {
		// Answer text is starting: reasoning for this step is over, so hide the
		// Thinking box. Cleared only when the block opens (not on every delta) so
		// any reasoning that interleaves with the answer can still surface.
		m.thinking = ""
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

// busyLabel returns the activity label for the working indicator. When a tool is
// executing, it returns "Running <display name>". Otherwise it defaults to
// "Working…" or "Pursuing goal…" depending on the goal state.
func (m *model) busyLabel() string {
	if m.runningTool != "" {
		return "Running " + toolDisplayName(m.runningTool)
	}
	cs, ok := m.composerStatus()
	if ok && cs.GoalActive {
		return "Pursuing goal…"
	}
	return "Working…"
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

// showWorkingIndicator reports whether the standalone "Working…" spinner row
// should render. The turn is busy from submit until StreamDone; the line shows
// only during genuine wait gaps (context prep, network, provider queueing,
// between tool rounds) and is suppressed whenever another element already owns
// the activity cue or visible progress makes it redundant:
//   - tool confirmation (the confirming card carries its own '?' menu),
//   - a running tool card (its header embeds the spinner),
//   - a visible thinking box (its border embeds the spinner),
//   - assistant text actively streaming into the scrollback (the words are the
//     feedback; a spinner would be noise) — but only while deltas are still
//     arriving. Once they pause (streamingSpinnerGrace) the spinner returns so
//     the silent wait for the model's next action still shows activity.
func (m *model) showWorkingIndicator() bool {
	if !m.busy {
		return false
	}
	if m.confirmReply != nil {
		return false
	}
	if m.activeCard != nil && m.activeCard.phase == toolRunning {
		return false
	}
	if m.thinkingBoxVisible() {
		return false
	}
	if m.openResponseIdx >= 0 && time.Since(m.lastTextDeltaAt) < streamingSpinnerGrace {
		return false
	}
	return true
}

// streamingSpinnerGrace is how long after the last assistant text delta the
// working spinner stays hidden. While deltas keep arriving the words are the
// feedback; once they stop for this long the turn is waiting on the model (or a
// tool round) so the spinner reappears even with the response block still open.
const streamingSpinnerGrace = 400 * time.Millisecond

func (m *model) wrapWidth() int {
	w := m.viewport.Width
	if w <= 0 {
		w = max(m.width-2, 1)
	}
	return w
}

func (m *model) syncViewportContent() {
	m.viewport.SetContent(m.renderScrollback(m.wrapWidth()))
	if m.followBottom {
		m.viewport.GotoBottom()
	}
}

// handleScrollKey scrolls the conversation viewport for the dedicated scroll
// keys (PgUp/PgDn for half pages, Shift+Up/Down for single lines). It reports
// true when key was a scroll command so handleKey can return early. Scrolling
// up unpins followBottom; reaching the bottom re-pins it.
func (m *model) handleScrollKey(key string) bool {
	switch key {
	case "pgup":
		m.viewport.HalfViewUp()
	case "pgdown":
		m.viewport.HalfViewDown()
	case "shift+up":
		m.viewport.LineUp(1)
	case "shift+down":
		m.viewport.LineDown(1)
	default:
		return false
	}
	m.followBottom = m.viewport.AtBottom()
	return true
}

// effectiveShowThinking reports whether the thinking box should be visible: the
// live session toggle once the user has pressed Ctrl+T, otherwise the resolved
// per-provider/model/global setting from the composer status.
func (m *model) effectiveShowThinking() bool {
	if m.thinkingToggled {
		return m.showThinking
	}
	if cs, ok := m.composerStatus(); ok {
		return cs.ShowThinking
	}
	return m.showThinking
}

// thinkingSavedMsg reports the result of persisting the thinking-box toggle off
// the Update goroutine (fire-and-forget; only errors are surfaced).
type thinkingSavedMsg struct{ err error }

// themeCycledMsg carries the new theme name (and any save error) back from the
// async ThemeController call so it is applied on the Update goroutine.
type themeCycledMsg struct {
	name string
	err  error
}

// toggleThinking flips the thinking-box visibility instantly (in-memory) and
// persists the global setting off the Update goroutine via a tea.Cmd so the disk
// write never blocks input. The visual change is applied here; only the save is
// async.
func (m *model) toggleThinking() (tea.Model, tea.Cmd) {
	newVal := !m.effectiveShowThinking()
	m.showThinking = newVal
	m.thinkingToggled = true
	state := "hidden"
	if newVal {
		state = "shown"
	}
	m.addBlock(roleInfo, "Thinking box "+state+" (Ctrl+T).")
	m.syncViewportContent()
	tc, ok := m.app.(ui.ThinkingController)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg { return thinkingSavedMsg{err: tc.SetShowThinking(newVal)} }
}

// streamContext returns the live turn context, falling back to a background
// context when the model has not captured one yet.
func (m *model) streamContext() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// startAppStream wires an app capability that returns a stream of events
// (mode/model switches) into the busy state with the given status label. An
// error surfaces via the terminal error banner without entering the busy state.
func (m *model) startAppStream(events <-chan ui.StreamEvent, err error, statusLeft string) (tea.Model, tea.Cmd) {
	if err != nil {
		_ = m.term.ShowError(err)
		return m, nil
	}
	if events == nil {
		return m, nil
	}
	m.busy = true
	m.status.Left = statusLeft
	m.stream = events
	m.streamGen++
	m.activeStreamGen = m.streamGen
	return m, waitStream(events, m.activeStreamGen)
}

// startModeSwitch switches directly to the named interaction mode via the app's
// SetModeByName capability (backs Alt+1..4), mirroring the Ctrl+Shift+M cycle.
func (m *model) startModeSwitch(name string) (tea.Model, tea.Cmd) {
	setter, ok := m.app.(interface {
		SetModeByName(context.Context, string) (<-chan ui.StreamEvent, error)
	})
	if !ok {
		return m, nil
	}
	events, err := setter.SetModeByName(m.streamContext(), name)
	return m.startAppStream(events, err, "mode")
}

// cycleTheme toggles the color theme via the app's ThemeController capability
// (Alt+T). The compute+persist call runs off the Update goroutine in a tea.Cmd
// (it touches disk); the new theme is applied live when the themeCycledMsg lands.
// It is a no-op when the app does not implement the capability (e.g. in tests).
func (m *model) cycleTheme() (tea.Model, tea.Cmd) {
	tc, ok := m.app.(ui.ThemeController)
	if !ok {
		return m, nil
	}
	return m, func() tea.Msg {
		name, err := tc.CycleTheme()
		return themeCycledMsg{name: name, err: err}
	}
}

// setMouse turns mouse-wheel reporting on or off at runtime and returns the tea
// command that applies it. When on, the conversation viewport scrolls with the
// wheel but native click-drag selection requires holding Shift; when off, native
// text selection works and scrollback uses the keyboard (PgUp/PgDn/Shift+arrows).
func (m *model) setMouse(on bool) tea.Cmd {
	m.mouseEnabled = on
	if on {
		m.addBlock(roleInfo, "Mouse scroll on (Alt+M); hold Shift to select text.")
		m.syncViewportContent()
		return tea.EnableMouseCellMotion
	}
	m.addBlock(roleInfo, "Mouse scroll off (Alt+M); text selection enabled.")
	m.syncViewportContent()
	return tea.DisableMouse
}

// applyMouseMode resolves a /mouse argument ("on"|"off"|"toggle") to the matching
// setMouse command. An empty or unknown value toggles.
func (m *model) applyMouseMode(mode string) tea.Cmd {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on":
		return m.setMouse(true)
	case "off":
		return m.setMouse(false)
	default:
		return m.setMouse(!m.mouseEnabled)
	}
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
	default:
		return "  ", m.th.Primary, m.th.Primary
	}
}

// renderBlock wraps a block's text and prefixes the first visual line with the
// role glyph; continuation lines are indented to align under it. Assistant
// responses are run through the lightweight markdown renderer; all other roles
// render as plain styled text.
func (m *model) renderBlock(blk scrollBlock, width int) []string {
	if blk.role == roleToolCard && blk.card != nil {
		return m.renderToolCard(blk.card, width)
	}

	glyph, prefix, body := m.roleStyle(blk.role)
	gw := lipgloss.Width(glyph)
	indent := strings.Repeat(" ", gw)

	var rendered []string
	switch {
	case blk.role == roleResponse:
		rendered = renderMarkdown(blk.text, max(width-gw, 1), m.th)
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
// inside it. Matches gemini-cli's 10-line input viewport.
const maxInputRows = 10

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
	return inputBoxHeight(inputContentLines(m.input))
}

func (m *model) bodyHeight() int {
	// Baseline 6 covers header, a single input row, and the two-line footer;
	// add the extra input rows when the box has grown for a multi-line prompt.
	chrome := 6 + m.suggestionRows() + m.statusRowRows() + m.activityRows() + separatorRows + (m.inputHeight() - 1)
	h := m.height - chrome
	if h < 3 {
		return 3
	}
	return h
}

// activityRows is the height of the composer activity area between the status
// row and the input: the thinking box when visible (it owns the spinner),
// otherwise the standalone working line. They are mutually exclusive in View.
func (m *model) activityRows() int {
	if rows := m.thinkingBoxRows(); rows > 0 {
		return rows
	}
	return m.workingRows()
}

// separatorRows is the fixed height of the dim rule drawn between the agent area
// and the input box (always one row), accounted for in bodyHeight.
const separatorRows = 1

// renderInputSeparator draws a dim horizontal rule visually separating the agent
// area above from the input box below.
func (m *model) renderInputSeparator() string {
	width := max(m.width, 1)
	return m.th.Dim.Render(strings.Repeat("─", width))
}

// workingRows is the height of the working/thinking indicator line (1 while the
// agent is working, else 0) so the viewport shrinks to make room for it.
func (m *model) workingRows() int {
	if m.showWorkingIndicator() {
		return 1
	}
	return 0
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

func waitStream(events <-chan ui.StreamEvent, gen uint64) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return streamEventMsg{gen: gen, event: ui.StreamEvent{Type: ui.StreamDone}}
		}
		return streamEventMsg{gen: gen, event: ev}
	}
}

type concurrentStreamEventMsg struct {
	gen   uint64
	event ui.StreamEvent
	ch    <-chan ui.StreamEvent
}

func waitConcurrentStream(events <-chan ui.StreamEvent, gen uint64) tea.Cmd {
	return func() tea.Msg {
		for ev := range events {
			if ev.Type == ui.StreamDone {
				continue
			}
			return concurrentStreamEventMsg{gen: gen, event: ev, ch: events}
		}
		return nil
	}
}

func drainStreamCmd(ch <-chan ui.StreamEvent) tea.Cmd {
	return func() tea.Msg {
		for range ch {
		}
		return turnDrainDoneMsg{}
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
		status.Detail = footerDetailWithShortcuts(status.Detail)
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

	status.Detail = footerDetailWithShortcuts(status.Detail)
	return status
}

// footerDetailWithShortcuts prepends OS-specific scroll hints to the footer
// detail line (the bottom row) so shortcuts stay visible even when the status
// row above the input is clipped or overlooked.
func footerDetailWithShortcuts(detail string) string {
	hints := scrollShortcutHints()
	if strings.TrimSpace(detail) == "" {
		return hints
	}
	return hints + "  ·  " + detail
}

func renderFooter(status ui.StatusBar, th theme.Theme, width int) string {
	// Fit the parts to the terminal width before styling. An over-wide footer
	// line soft-wraps and corrupts Bubble Tea's frame accounting, leaving ghost
	// footer/status rows; keep the right-aligned model+usage segment intact.
	left, right := fitLeftRight(status.Left, status.Right, width)

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

	detailText := ansi.Truncate(status.Detail, max(width, 1), "")
	var detail string
	if len(th.TitleGradient) > 0 {
		detail = th.GradientText(detailText, th.Secondary, th.TitleGradient)
	} else {
		detail = th.Secondary.Render(detailText)
	}
	return line + "\n" + detail
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
