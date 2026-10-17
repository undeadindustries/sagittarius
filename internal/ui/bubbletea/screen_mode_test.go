package bubbletea

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/screendialog"
	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

type screenStoreApp struct {
	quitApp
	marked  bool
	saved   string
	saveErr error
}

func (s *screenStoreApp) MarkSSHCopyTipShown() error {
	s.marked = true
	return nil
}

func (s *screenStoreApp) SaveScreenLayout(mode string) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = mode
	return nil
}

func remoteGetenv(key string) string {
	if key == "SSH_CONNECTION" {
		return "1.2.3.4 1 5.6.7.8 22"
	}
	return ""
}

func cmdType(cmd tea.Cmd) string {
	if cmd == nil {
		return "nil"
	}
	return fmt.Sprintf("%T", cmd())
}

func TestInlinePrintKeepsStyleFramesAndCode(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.th = theme.Default()
	m.addBlock(roleUser, "hello")
	text, n := m.inlineChunk(0, true)
	if n != 1 || !strings.Contains(text, "You") || !strings.Contains(text, "\x1b") {
		t.Fatalf("styled user block = %q", text)
	}

	card := &toolCard{phase: toolSuccess, toolName: "shell", displayName: "Shell"}
	m.blocks = append(m.blocks, scrollBlock{role: roleToolCard, card: card})
	text, n = m.inlineChunk(0, true)
	if n != 2 || !strings.Contains(text, "Shell") || !strings.Contains(text, "╭") {
		t.Fatalf("card frame missing:\n%s", text)
	}

	m2 := newInlineTestModel()
	m2.width = 40
	m2.viewport.Width = 38
	m2.addBlock(roleResponse, "```\n"+strings.Repeat("a", 200)+"\n```")
	code, _ := m2.inlineChunk(0, true)
	if plain := ansi.Strip(code); !strings.Contains(plain, strings.Repeat("a", 200)) {
		t.Fatalf("code line was not rejoined:\n%s", plain)
	}

	m3 := newInlineTestModel()
	m3.addBlock(roleResponse, strings.Repeat("word ", 80))
	prose, _ := m3.inlineChunk(0, true)
	if !strings.Contains(prose, "\n") {
		t.Fatal("prose should stay word-wrapped")
	}
}

func TestInlineSpacingMatchesScrollback(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.addBlock(roleUser, "one")
	m.addBlock(roleInfo, "note")
	m.addBlock(roleUser, "two")
	text, n := m.inlineChunk(0, true)
	if n != 3 {
		t.Fatalf("n = %d", n)
	}
	if strings.Count(text, "\n\n") != 1 {
		t.Fatalf("want one blank line (before the second user block):\n%q", text)
	}
}

func TestInlineCommitWaitsForWidth(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.width = 0
	m.addBlock(roleUser, "hi")
	if cmd := m.inlineCommitCmd(); cmd != nil {
		t.Fatal("printed before the first window size")
	}
}

func TestInlineBootOnFirstResize(t *testing.T) {
	t.Parallel()
	m := newModel(ui.Options{ThemeName: "greyscale", Inline: true, HideBanner: true, HideTips: true}, quitApp{}, NewTerminal(ui.Options{}))
	if !m.inlineBootPending || m.width != 0 {
		t.Fatalf("boot pending=%v width=%d", m.inlineBootPending, m.width)
	}
	updated, _ := m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	booted := updated.(*model)
	if booted.inlineBootPending {
		t.Fatal("first resize should consume the boot")
	}
	_, cmd := booted.handleWindowSize(tea.WindowSizeMsg{Width: 100, Height: 24})
	if cmd != nil {
		t.Fatal("second resize should not print")
	}
}

func TestScreenSwitchCommandsAndWait(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.inline = false
	m.mouseEnabled = true
	m.mouseWanted = true
	m.addBlock(roleUser, "hello")
	m.inline = true
	cmds := m.inlineSwitchCmds()
	got := []string{cmdType(cmds[0]), cmdType(cmds[1]), cmdType(cmds[2]), cmdType(cmds[3])}
	for i, want := range []string{"exitAltScreen", "disableMouse", "printLine", "inlinePrintedMsg"} {
		if !strings.Contains(got[i], want) {
			t.Fatalf("cmd %d = %s, want %s (all %v)", i, got[i], want, got)
		}
	}
	if !m.printInFlight {
		t.Fatal("switch did not reserve the print")
	}

	waiting := newInlineTestModel()
	waiting.printInFlight = true
	waiting.mouseWanted = true
	if cmd := waiting.requestFullscreen(); cmd != nil || !waiting.pendingFullscreen {
		t.Fatal("fullscreen switch should wait for the print")
	}
	next, cmd := waiting.handleInlinePrinted(inlinePrintedMsg{gen: waiting.printGeneration})
	updated := next.(*model)
	if updated.inline || !updated.mouseEnabled || cmd == nil {
		t.Fatalf("after ack inline=%v mouse=%v cmd=%v", updated.inline, updated.mouseEnabled, cmd != nil)
	}
}

func TestScreenSwitchWhileDialogOpenDefersPrint(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.inline = false
	empty := screendialog.New("Option")
	m.screenOverlay = &empty
	if cmd := m.requestInline(); cmd != nil || !m.inline {
		t.Fatal("an open dialog should only flip state")
	}
	m.screenOverlay = nil
	if cmd := m.screenFollowUp(false, true); cmd == nil {
		t.Fatal("closing the dialog should start the inline print")
	}
}

func TestQuitAfterInlinePrintsRemainder(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	m.addBlock(roleUser, "kept")
	m.usedInline = true
	m.inline = false
	if cmd := m.beginQuit(); cmd == nil {
		t.Fatal("expected a quit command")
	}
	if m.committed != len(m.blocks) {
		t.Fatalf("committed = %d, want %d", m.committed, len(m.blocks))
	}
}

func TestSSHCopyTipOnceAndSavedChoice(t *testing.T) {
	t.Parallel()
	store := &screenStoreApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, store, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.getenv = remoteGetenv
	m.inlineBootPending = false

	m.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if !m.copyTipPending {
		t.Fatal("remote copy should arm the dialog")
	}
	m.busy = true
	if cmd := m.maybeOpenCopyTip(); cmd != nil || m.screenOverlay != nil {
		t.Fatal("dialog opened while a turn was running")
	}
	m.busy = false
	cmd := m.maybeOpenCopyTip()
	if m.screenOverlay == nil || cmd == nil {
		t.Fatal("idle UI should open the dialog and persist the shown flag")
	}
	if _, ok := cmd().(sshCopyTipSavedMsg); !ok {
		t.Fatal("mark command did not report")
	}
	if !store.marked {
		t.Fatal("shown flag was not saved")
	}
	m.handleClipboardResult(clipboardResultMsg{text: "y", osc52: true})
	if m.copyTipPending {
		t.Fatal("dialog armed twice")
	}

	local := newModel(ui.Options{ThemeName: "greyscale"}, store, NewTerminal(ui.Options{}))
	local.getenv = func(string) string { return "" }
	local.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if local.copyTipPending {
		t.Fatal("local copy should not arm the dialog")
	}

	shown := newModel(ui.Options{ThemeName: "greyscale", SSHCopyTipShown: true}, store, NewTerminal(ui.Options{}))
	shown.width = 80
	shown.getenv = remoteGetenv
	shown.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if shown.copyTipPending {
		t.Fatal("a seen flag should suppress the dialog")
	}
}

func TestSSHCopyTipChoiceSavesAuto(t *testing.T) {
	t.Parallel()
	store := &screenStoreApp{}
	m := newModel(ui.Options{ThemeName: "greyscale"}, store, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.inlineBootPending = false
	d := screendialog.New("Option").SetSize(80, 24)
	m.screenOverlay = &d
	var next tea.Model = m
	var cmd tea.Cmd
	next, cmd = next.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		_ = cmd()
	}
	next, cmd = next.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := next.(*model)
	if updated.screenOverlay != nil || !updated.inline {
		t.Fatalf("overlay=%v inline=%v", updated.screenOverlay != nil, updated.inline)
	}
	if cmd == nil {
		t.Fatal("expected the switch and save commands")
	}
	// The batch runs the save. Execute it by asking the model directly so the
	// assertion does not depend on Batch's goroutines.
	saved := updated.saveScreenLayoutCmd(config.UIScreenAuto)()
	msg, ok := saved.(screenLayoutSavedMsg)
	if !ok || msg.err != nil || store.saved != config.UIScreenAuto {
		t.Fatalf("save = %#v stored %q", saved, store.saved)
	}
	updated.handleScreenLayoutSaved(msg)
	if !strings.Contains(updated.blocks[len(updated.blocks)-1].text, "New SSH sessions start inline") {
		t.Fatalf("info = %q", updated.blocks[len(updated.blocks)-1].text)
	}

	store.saveErr = errors.New("disk full")
	failed := updated.saveScreenLayoutCmd(config.UIScreenAuto)().(screenLayoutSavedMsg)
	updated.handleScreenLayoutSaved(failed)
	if !strings.Contains(updated.blocks[len(updated.blocks)-1].text, "disk full") {
		t.Fatalf("error block = %q", updated.blocks[len(updated.blocks)-1].text)
	}
}

func TestShortcutHintsFollowMode(t *testing.T) {
	t.Parallel()
	m := newInlineTestModel()
	if m.scrollShortcutHints() != inlineShortcuts {
		t.Fatalf("inline hints = %q", m.scrollShortcutHints())
	}
	m.inline = false
	if !strings.Contains(m.scrollShortcutHints(), "Alt+M") {
		t.Fatalf("fullscreen hints = %q", m.scrollShortcutHints())
	}
}

func TestAppleTerminalHintIsFn(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.getenv = func(key string) string {
		if key == "TERM_PROGRAM" {
			return "Apple_Terminal"
		}
		if key == "SSH_CONNECTION" {
			return "1 2 3 4"
		}
		return ""
	}
	m.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if !strings.Contains(m.blocks[0].text, clipboard.NativeSelectionHint(m.getenv)) {
		t.Fatalf("copy text = %q", m.blocks[0].text)
	}
}
