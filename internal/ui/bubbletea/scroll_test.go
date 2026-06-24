package bubbletea

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

// fillScrollback adds enough blocks that the content overflows the viewport.
func fillScrollback(m *model, n int) {
	for i := 0; i < n; i++ {
		m.addBlock(roleInfo, "scrollback line")
	}
	m.syncViewportContent()
}

func TestScrollUpUnpinsFollowBottom(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	fillScrollback(m, 100)

	if !m.followBottom || !m.viewport.AtBottom() {
		t.Fatalf("precondition: followBottom=%v atBottom=%v", m.followBottom, m.viewport.AtBottom())
	}

	if !m.handleScrollKey("pgup") {
		t.Fatal("pgup should be handled as a scroll key")
	}
	if m.followBottom {
		t.Fatal("scrolling up should unpin followBottom")
	}
	if m.viewport.AtBottom() {
		t.Fatal("viewport should no longer be at the bottom after pgup")
	}
}

func TestUnpinnedViewportDoesNotAutoScroll(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	fillScrollback(m, 100)
	m.handleScrollKey("pgup")

	// New streamed content must not yank an unpinned viewport back to the bottom.
	m.handleStream(ui.StreamEvent{Type: ui.StreamInfo, Text: "new line while reading"})
	if m.viewport.AtBottom() {
		t.Fatal("unpinned viewport should not auto-scroll to bottom on new content")
	}
}

func TestScrollBackToBottomRepins(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	fillScrollback(m, 100)
	m.handleScrollKey("pgup")
	if m.followBottom {
		t.Fatal("setup: should be unpinned after pgup")
	}

	for i := 0; i < 100 && !m.viewport.AtBottom(); i++ {
		m.handleScrollKey("pgdown")
	}
	if !m.viewport.AtBottom() {
		t.Fatal("pgdown should eventually reach the bottom")
	}
	if !m.followBottom {
		t.Fatal("returning to the bottom should re-pin followBottom")
	}
}

func TestSubmitResetsFollowBottom(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	fillScrollback(m, 100)
	m.handleScrollKey("pgup")
	if m.followBottom {
		t.Fatal("setup: should be unpinned")
	}
	m.handleSubmit("hello")
	if !m.followBottom {
		t.Fatal("submitting a new turn should re-pin to the bottom")
	}
}

func TestHandleKeyRoutesScrollKeys(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleWindowSize(tea.WindowSizeMsg{Width: 80, Height: 24})
	fillScrollback(m, 100)
	m.handleKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.followBottom {
		t.Fatal("PgUp through handleKey should unpin followBottom")
	}
	// The scroll key must not be inserted into the input.
	if m.input.Value() != "" {
		t.Fatalf("scroll key leaked into input: %q", m.input.Value())
	}
}
