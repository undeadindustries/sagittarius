package bubbletea

import (
	"context"
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

func (a *shortcutApp) CycleModelReverse(context.Context) (<-chan ui.StreamEvent, error) {
	a.reverseCalls++
	return doneStream(), nil
}

func (a *shortcutApp) CycleTheme() (string, error) {
	a.themeCalls++
	return a.themeReturn, nil
}

func newShortcutModel(app ui.App) *model {
	m := newModel(ui.Options{ThemeName: "greyscale"}, app, NewTerminal(ui.Options{}))
	m.width, m.height = 80, 24
	return m
}

func TestSetMouseTogglesAndReturnsCommand(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	if m.mouseEnabled {
		t.Fatal("mouse should be off by default")
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
		{"toggle", true}, // from off -> on
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
	m.cycleTheme()
	if app.themeCalls != 1 {
		t.Fatalf("CycleTheme calls = %d, want 1", app.themeCalls)
	}
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
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}, Alt: true})
	if app.themeCalls != 1 {
		t.Fatalf("Alt+T should call CycleTheme once, got %d", app.themeCalls)
	}

	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'†'}})
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
