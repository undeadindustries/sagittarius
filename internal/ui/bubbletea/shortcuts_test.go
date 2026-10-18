package bubbletea

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// shortcutApp implements ui.App plus the capabilities the new keyboard shortcuts
// rely on: SetModeByName (Alt+1..4), CycleModelReverse (Ctrl+Shift+P), and
// ui.ThemeController (Alt+T). It records what was requested for assertions.
type shortcutApp struct {
	modeName     string
	modeCalls    int
	cycleCalls   int
	reverseCalls int
	themeReturn  string
	themeCalls   int
}

func (*shortcutApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent)
	close(ch)
	return ch, nil
}

func doneStream() <-chan ui.StreamEvent {
	ch := make(chan ui.StreamEvent, 1)
	ch <- ui.StreamEvent{Type: ui.StreamDone}
	close(ch)
	return ch
}

func (a *shortcutApp) SetModeByName(_ context.Context, name string) (<-chan ui.StreamEvent, error) {
	a.modeName = name
	a.modeCalls++
	return doneStream(), nil
}

func (a *shortcutApp) CycleInteractionMode(context.Context) (<-chan ui.StreamEvent, error) {
	a.cycleCalls++
	return doneStream(), nil
}

func (a *shortcutApp) CycleModelReverse(context.Context) (<-chan ui.StreamEvent, error) {
	a.reverseCalls++
	return doneStream(), nil
}

func (a *shortcutApp) CycleTheme() (string, error) {
	a.themeCalls++
	return a.themeReturn, nil
}

// debugExportApp implements ui.App and ui.RequestDebugExporter.
type debugExportApp struct {
	path  string
	err   error
	calls int
}

func (*debugExportApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	return doneStream(), nil
}

func (a *debugExportApp) ExportRequestDebug() (string, error) {
	a.calls++
	return a.path, a.err
}

func TestCtrlShiftDExportsRequestWhileBusy(t *testing.T) {
	t.Parallel()
	app := &debugExportApp{path: "/tmp/sagittarius-request-test.json"}
	m := newShortcutModel(app)
	m.busy = true
	before := len(m.blocks)

	// KeyRunes of the binding string: bubbletea has no KeyCtrlShiftD type;
	// handleKey matches msg.String(), which for KeyRunes is the rune text.
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(requestDebugKey)})
	if cmd == nil {
		t.Fatal("Ctrl+Shift+D should return a command")
	}
	if !m.busy {
		t.Fatal("export must not cancel the in-flight turn")
	}
	msg := cmd()
	if app.calls != 1 {
		t.Fatalf("ExportRequestDebug calls = %d, want 1", app.calls)
	}
	m.Update(msg)
	if len(m.blocks) != before+1 {
		t.Fatalf("expected one info block, got %d new", len(m.blocks)-before)
	}
	got := stripANSI(m.renderScrollback(80))
	if !strings.Contains(got, "Wrote last request to /tmp/sagittarius-request-test.json") {
		t.Fatalf("export info missing from scrollback:\n%s", got)
	}
}

func TestCtrlShiftDExportErrorSurfaces(t *testing.T) {
	t.Parallel()
	app := &debugExportApp{err: errors.New("no provider request recorded yet — send a message first")}
	m := newShortcutModel(app)
	before := len(m.blocks)
	_, cmd := m.exportRequestDebug()
	if cmd == nil {
		t.Fatal("exportRequestDebug should return a command")
	}
	m.Update(cmd())
	if len(m.blocks) != before+1 {
		t.Fatalf("expected one error block, got %d new", len(m.blocks)-before)
	}
	if !strings.Contains(stripANSI(m.renderScrollback(80)), "no provider request recorded yet") {
		t.Fatal("error text missing from scrollback")
	}
}

func TestCtrlShiftDWithoutCapability(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	before := len(m.blocks)
	_, cmd := m.exportRequestDebug()
	if cmd != nil {
		t.Fatal("no exporter should not return a command")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("expected an unavailable notice, got %d new blocks", len(m.blocks)-before)
	}
	if !strings.Contains(stripANSI(m.renderScrollback(80)), "unavailable") {
		t.Fatal("unavailable notice missing")
	}
}

func newShortcutModel(app ui.App) *model {
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width, m.height = 80, 24
	return m
}

func TestSetMouseTogglesAndReturnsCommand(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	if !m.mouseEnabled {
		t.Fatal("mouse should be on by default in the interactive TUI")
	}
	if cmd := m.setMouse(true); cmd == nil {
		t.Fatal("enabling mouse should return a command")
	}
	if !m.mouseEnabled {
		t.Fatal("mouseEnabled should be true after setMouse(true)")
	}
	if cmd := m.setMouse(false); cmd == nil {
		t.Fatal("disabling mouse should return a command")
	}
	if m.mouseEnabled {
		t.Fatal("mouseEnabled should be false after setMouse(false)")
	}
}

func TestApplyMouseModeParsesArgument(t *testing.T) {
	t.Parallel()
	cases := []struct {
		mode string
		want bool
	}{
		{"on", true},
		{"off", false},
		{"toggle", false}, // from the on default -> off
	}
	for _, tc := range cases {
		m := newTestModel()
		m.applyMouseMode(tc.mode)
		if m.mouseEnabled != tc.want {
			t.Errorf("applyMouseMode(%q): mouseEnabled = %v, want %v", tc.mode, m.mouseEnabled, tc.want)
		}
	}
}

func TestAltMKeyTogglesMouse(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.mouseEnabled = false
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true})
	if cmd == nil {
		t.Fatal("Alt+M should return a tea command")
	}
	if !m.mouseEnabled {
		t.Fatal("Alt+M should enable mouse capture")
	}

	m.mouseEnabled = false
	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'µ'}})
	if cmd == nil || !m.mouseEnabled {
		t.Fatal("Mac Option+M (µ) should enable mouse capture")
	}
}

func TestCycleThemeAppliesAndPersists(t *testing.T) {
	t.Parallel()
	app := &shortcutApp{themeReturn: "default"}
	m := newShortcutModel(app)
	before := len(m.blocks)
	// cycleTheme defers the compute+persist to a Cmd; executing it yields the
	// themeCycledMsg which Update applies (theme swap + info block).
	_, cmd := m.cycleTheme()
	if cmd == nil {
		t.Fatal("cycleTheme should return a command")
	}
	msg := cmd()
	if app.themeCalls != 1 {
		t.Fatalf("CycleTheme calls = %d, want 1", app.themeCalls)
	}
	m.Update(msg)
	if len(m.blocks) != before+1 {
		t.Fatalf("cycleTheme should add one info block, got %d new", len(m.blocks)-before)
	}
	if !strings.Contains(stripANSI(m.renderScrollback(80)), "Theme → default") {
		t.Fatal("theme info block missing from scrollback")
	}
}

func TestAltTKeyCyclesTheme(t *testing.T) {
	t.Parallel()
	app := &shortcutApp{themeReturn: "greyscale"}
	m := newShortcutModel(app)
	// The key handler returns the cycle Cmd; execute it to drive CycleTheme.
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}, Alt: true})
	if cmd == nil {
		t.Fatal("Alt+T should return a command")
	}
	cmd()
	if app.themeCalls != 1 {
		t.Fatalf("Alt+T should call CycleTheme once, got %d", app.themeCalls)
	}

	_, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'†'}})
	if cmd == nil {
		t.Fatal("Mac Option+T (†) should return a command")
	}
	cmd()
	if app.themeCalls != 2 {
		t.Fatalf("Mac Option+T (†) should call CycleTheme, got %d", app.themeCalls)
	}
}

func TestStartModeSwitchEntersBusy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		key  rune
		mac  rune
		want string
	}{
		{'1', '¡', "agent"},
		{'2', '™', "plan"},
		{'3', '£', "ask"},
		{'4', '¢', "debug"},
	}
	for _, tc := range cases {
		app := &shortcutApp{}
		m := newShortcutModel(app)
		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}, Alt: true})
		if app.modeName != tc.want {
			t.Errorf("Alt+%c: mode = %q, want %q", tc.key, app.modeName, tc.want)
		}
		if !m.busy || m.stream == nil || cmd == nil {
			t.Errorf("Alt+%c: expected busy stream state (busy=%v stream=%v)", tc.key, m.busy, m.stream != nil)
		}

		app2 := &shortcutApp{}
		m2 := newShortcutModel(app2)
		_, cmd2 := m2.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.mac}})
		if app2.modeName != tc.want {
			t.Errorf("Mac Option+%c (%c): mode = %q, want %q", tc.key, tc.mac, app2.modeName, tc.want)
		}
		if !m2.busy || m2.stream == nil || cmd2 == nil {
			t.Errorf("Mac Option+%c (%c): expected busy stream state", tc.key, tc.mac)
		}
	}
}

func TestStartModeSwitchNoCapabilityIsNoop(t *testing.T) {
	t.Parallel()
	m := newTestModel() // quitApp does not implement SetModeByName
	if _, cmd := m.startModeSwitch("plan"); cmd != nil {
		t.Fatal("startModeSwitch without capability should be a no-op")
	}
	if m.busy {
		t.Fatal("no-op mode switch must not enter busy state")
	}
}

func TestShiftTabCyclesMode(t *testing.T) {
	t.Parallel()
	app := &shortcutApp{}
	m := newShortcutModel(app)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if app.cycleCalls != 1 {
		t.Fatalf("Shift+Tab should call CycleInteractionMode once, got %d", app.cycleCalls)
	}
	if !m.busy || m.stream == nil || cmd == nil {
		t.Fatalf("Shift+Tab should enter busy stream state (busy=%v stream=%v)", m.busy, m.stream != nil)
	}
}

func TestShiftTabIgnoredWhileBusy(t *testing.T) {
	t.Parallel()
	app := &shortcutApp{}
	m := newShortcutModel(app)
	m.busy = true
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if app.cycleCalls != 0 {
		t.Fatalf("Shift+Tab mid-turn must not cycle mode, got %d calls", app.cycleCalls)
	}
	if cmd != nil {
		t.Fatal("Shift+Tab mid-turn should be a no-op (nil cmd)")
	}
}

// sessionKeyApp implements ui.ReasoningController (Alt+R) and
// ui.ApprovalController (Alt+Y), counting calls for assertions.
type sessionKeyApp struct {
	reasoningCalls int
	reasoningErr   error
	yoloCalls      int
	yolo           bool
}

func (*sessionKeyApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	return doneStream(), nil
}

func (a *sessionKeyApp) CycleReasoningEffort() (string, string, error) {
	a.reasoningCalls++
	if a.reasoningErr != nil {
		return "", "", a.reasoningErr
	}
	return "high", "Reasoning effort → high for test-model this session (Alt+R).", nil
}

func (a *sessionKeyApp) ToggleYolo() (string, bool, error) {
	a.yoloCalls++
	a.yolo = !a.yolo
	if a.yolo {
		return "yolo", true, nil
	}
	return "autoEdit", false, nil
}

func TestSessionKeysRunCapability(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		key       tea.KeyMsg
		wantText  string
		reasoning bool
	}{
		{"Alt+R", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true}, "Reasoning effort → high", true},
		{"Mac Option+R", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'®'}}, "Reasoning effort → high", true},
		{"Ctrl+Shift+T", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(reasoningKeyCtrlShift)}, "Reasoning effort → high", true},
		{"Alt+Y", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true}, "Yolo on for this session", false},
		{"Mac Option+Y", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'¥'}}, "Yolo on for this session", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			app := &sessionKeyApp{}
			m := newShortcutModel(app)
			_, cmd := m.handleKey(tc.key)
			if cmd == nil {
				t.Fatalf("%s should return a command", tc.name)
			}
			m.Update(cmd())
			calls := app.yoloCalls
			if tc.reasoning {
				calls = app.reasoningCalls
			}
			if calls != 1 {
				t.Fatalf("%s capability calls = %d, want 1", tc.name, calls)
			}
			if got := stripANSI(m.renderScrollback(120)); !strings.Contains(got, tc.wantText) {
				t.Fatalf("%s info missing from scrollback:\n%s", tc.name, got)
			}
			if m.input.Value() != "" {
				t.Fatalf("%s leaked into the composer: %q", tc.name, m.input.Value())
			}
		})
	}
}

func TestSessionKeysIgnoredWhileBusy(t *testing.T) {
	t.Parallel()
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'r'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'®'}},
		{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'¥'}},
	} {
		app := &sessionKeyApp{}
		m := newShortcutModel(app)
		m.busy = true
		if _, cmd := m.handleKey(key); cmd != nil {
			t.Errorf("%s mid-turn should be a no-op", key.String())
		}
		if app.reasoningCalls+app.yoloCalls != 0 {
			t.Errorf("%s mid-turn reached the app", key.String())
		}
		if m.input.Value() != "" {
			t.Errorf("%s mid-turn leaked into the composer: %q", key.String(), m.input.Value())
		}
	}
}

func TestSessionKeysWithoutCapabilityAreNoops(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	if _, cmd := m.cycleReasoningEffort(); cmd != nil {
		t.Error("cycleReasoningEffort without capability should be a no-op")
	}
	if _, cmd := m.toggleYolo(); cmd != nil {
		t.Error("toggleYolo without capability should be a no-op")
	}
}

func TestReasoningCycleErrorSurfaces(t *testing.T) {
	t.Parallel()
	app := &sessionKeyApp{reasoningErr: errors.New("test-model has no reasoning levels to switch between")}
	m := newShortcutModel(app)
	_, cmd := m.cycleReasoningEffort()
	m.Update(cmd())
	if !strings.Contains(stripANSI(m.renderScrollback(120)), "no reasoning levels") {
		t.Fatal("reasoning error missing from scrollback")
	}
}

func TestYoloToggleText(t *testing.T) {
	t.Parallel()
	if got := yoloToggleText("yolo", true); !strings.Contains(got, "run without asking") {
		t.Errorf("yolo on text = %q", got)
	}
	if got := yoloToggleText("autoEdit", false); !strings.HasSuffix(got, "back to autoEdit.") {
		t.Errorf("yolo off text = %q", got)
	}
}
