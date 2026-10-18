package bubbletea

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// updateErrorDeadline bounds each error path. Reaching it means the path
// called Program.Send from inside Update and is blocked for good.
const updateErrorDeadline = 2 * time.Second

// TestUpdateErrorPathsNeverSendToProgram is the regression for a TUI freeze:
// error branches inside Update called Terminal.ShowError, which calls
// Program.Send. That channel is unbuffered and only the event loop, which is
// busy running Update, drains it. The Terminal here holds a real Program that
// is never started, so any Send blocks exactly as it would in production.
func TestUpdateErrorPathsNeverSendToProgram(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom: test failure")
	cases := []struct {
		name string
		run  func(m *model)
	}{
		{"startAppStream (Ctrl+/, Shift+Tab, Alt+1..4)", func(m *model) { m.startAppStream(nil, boom, "model") }},
		{"thinking save", func(m *model) { m.Update(thinkingSavedMsg{err: boom}) }},
		{"theme cycle", func(m *model) { m.Update(themeCycledMsg{err: boom}) }},
		{"reasoning cycle", func(m *model) { m.Update(reasoningCycledMsg{err: boom}) }},
		{"yolo toggle", func(m *model) { m.Update(yoloToggledMsg{err: boom}) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			term := NewTerminal(ui.Options{})
			m := newModel(ui.Options{ThemeName: "greyscale"}, &shortcutApp{}, term)
			m.width, m.height = 80, 24
			term.program = tea.NewProgram(m)
			term.model = m

			done := make(chan struct{})
			go func() {
				defer close(done)
				tc.run(m)
			}()
			select {
			case <-done:
			case <-time.After(updateErrorDeadline):
				t.Fatal("error path blocked: it sent to the Program from inside Update")
			}
			if got := stripANSI(m.renderScrollback(80)); !strings.Contains(got, "boom: test failure") {
				t.Fatalf("error not shown in the scrollback:\n%s", got)
			}
		})
	}
}
