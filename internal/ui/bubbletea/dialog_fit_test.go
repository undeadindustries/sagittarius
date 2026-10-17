package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestDialogsFitTerminalHeight opens dialogs on short terminals, inline and
// full screen. Bubble Tea keeps the bottom of a frame taller than the screen,
// so any overflow would hide the dialog title; the view must never exceed the
// height and must keep its title line.
func TestDialogsFitTerminalHeight(t *testing.T) {
	t.Parallel()
	dialogs := []struct {
		kind  ui.DialogKind
		title string
	}{
		{kind: ui.DialogProviders, title: "Providers"},
		{kind: ui.DialogModels, title: "Model Settings"},
	}
	// 12 rows is enough for FitList to window the rows; 5 rows forces the
	// host safety net to clip whatever is left.
	for _, height := range []int{12, 5} {
		for _, inline := range []bool{false, true} {
			for _, d := range dialogs {
				m := newDialogModel()
				m.inline = inline
				m.height = height
				m.handleStream(ui.StreamEvent{Type: ui.StreamOpenDialog, Dialog: d.kind})
				view := ansi.Strip(m.View())
				lines := strings.Split(view, "\n")
				if len(lines) > height {
					t.Errorf("%s height=%d inline=%v: %d lines, want <= %d:\n%s",
						d.title, height, inline, len(lines), height, view)
				}
				if !strings.Contains(view, d.title) {
					t.Errorf("%s height=%d inline=%v: title scrolled away:\n%s", d.title, height, inline, view)
				}
				if clipped := strings.Contains(view, "enlarge the terminal"); clipped != (height < 12) {
					t.Errorf("%s height=%d inline=%v: clipped=%v, want %v", d.title, height, inline, clipped, height < 12)
				}
			}
		}
	}
}
