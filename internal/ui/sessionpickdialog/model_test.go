package sessionpickdialog

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fixedDeps satisfies Deps with a canned catalog so tests never touch disk.
type fixedDeps struct {
	cat Catalog
	err error
}

func (f fixedDeps) Load(context.Context) (Catalog, error) { return f.cat, f.err }

var testNow = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

// loadedModel returns a dialog whose catalog has already arrived, the state
// the parent sees after the Load command completes.
func loadedModel(t *testing.T, cat Catalog) Model {
	t.Helper()
	m, _ := New(context.Background(), fixedDeps{cat: cat})
	m.now = func() time.Time { return testNow }
	m = m.SetSize(100, 40) // real terminals always size the overlay; wrapping at width 0 would split phrases
	next, _ := m.Update(loadMsg{catalog: cat})
	return next
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m Model, k tea.KeyMsg) Model {
	t.Helper()
	next, _ := m.Update(k)
	return next
}

func sampleCatalog() Catalog {
	return Catalog{
		Sessions: []SessionEntry{
			{Index: 1, ID: "sess-old", Title: "Older work", LastUpdated: testNow.Add(-26 * time.Hour), MessageCount: 4},
			{Index: 2, ID: "sess-current", Title: "Now", LastUpdated: testNow.Add(-2 * time.Minute), MessageCount: 9, Branch: "main"},
			{Index: 3, ID: "sess-busy", Title: "Elsewhere", LastUpdated: testNow.Add(-3 * time.Hour), MessageCount: 2, Live: true},
		},
		Checkpoints: []string{"alpha", "beta"},
		CurrentID:   "sess-current",
	}
}

func TestNewStartsLoadingAndCommandIsNonNil(t *testing.T) {
	t.Parallel()
	m, cmd := New(context.Background(), fixedDeps{})
	if !m.loading {
		t.Fatal("new dialog must start in the loading state")
	}
	if cmd == nil {
		t.Fatal("New must return a load command so the catalog arrives off the UI goroutine")
	}
	if !strings.Contains(m.View(), "Loading sessions") {
		t.Errorf("loading view missing its status line")
	}
}

func TestEscCancelsWithNoSubmitLine(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	m = press(t, m, key("esc"))
	if !m.Done() {
		t.Fatal("Esc must close the dialog")
	}
	if m.SubmitLine() != "" {
		t.Errorf("cancel produced submit line %q, want none", m.SubmitLine())
	}
}

func TestEscClosesWhileStillLoading(t *testing.T) {
	t.Parallel()
	m, _ := New(context.Background(), fixedDeps{})
	m = press(t, m, key("esc"))
	if !m.Done() {
		t.Fatal("Esc must close the dialog even mid-load; the menu must never trap the user")
	}
}

func TestEnterOnSessionSubmitsFullIDResume(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	// Rows are sessions in /chat list order; the first one is the old session.
	m = press(t, m, key("enter"))
	if !m.Done() {
		t.Fatal("Enter on a switchable session must close the dialog")
	}
	if got, want := m.SubmitLine(), "/chat resume sess-old"; got != want {
		t.Errorf("SubmitLine = %q, want %q", got, want)
	}
}

func TestEnterOnCurrentSessionStaysOpen(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	m = press(t, m, key("down")) // sess-current
	m = press(t, m, key("enter"))
	if m.Done() {
		t.Fatal("Enter on the current session must not close the dialog")
	}
	if m.SubmitLine() != "" {
		t.Errorf("current-session pick submitted %q, want nothing", m.SubmitLine())
	}
	if !strings.Contains(m.View(), noticeCurrentSession) {
		t.Errorf("current-session pick must explain itself on the dialog")
	}
}

func TestEnterOnLiveSessionStaysOpen(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	m = press(t, m, key("down"))
	m = press(t, m, key("down")) // sess-busy (Live)
	m = press(t, m, key("enter"))
	if m.Done() {
		t.Fatal("Enter on a session open elsewhere must not close the dialog")
	}
	if m.SubmitLine() != "" {
		t.Errorf("live-session pick submitted %q, want nothing", m.SubmitLine())
	}
	if !strings.Contains(m.View(), "another terminal") {
		t.Errorf("live-session pick must say why it refused")
	}
}

func TestEnterOnCheckpointSubmitsTagResume(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	// Three sessions, then checkpoint "alpha" at row index 3.
	for range 3 {
		m = press(t, m, key("down"))
	}
	m = press(t, m, key("enter"))
	if got, want := m.SubmitLine(), "/chat resume alpha"; got != want {
		t.Errorf("SubmitLine = %q, want %q", got, want)
	}
}

func TestCursorWrapsBothDirections(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	n := len(m.rows)
	m = press(t, m, key("up")) // wrap from 0 to last
	if m.cursor != n-1 {
		t.Errorf("up from top: cursor = %d, want %d", m.cursor, n-1)
	}
	m = press(t, m, key("down")) // wrap back to 0
	if m.cursor != 0 {
		t.Errorf("down from bottom: cursor = %d, want 0", m.cursor)
	}
	m = press(t, m, key("j"))
	m = press(t, m, key("k"))
	if m.cursor != 0 {
		t.Errorf("j then k: cursor = %d, want 0", m.cursor)
	}
}

func TestEmptyCatalogShowsEmptyStateAndEnterCloses(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, Catalog{})
	if !strings.Contains(m.View(), "No previous sessions or checkpoints.") {
		t.Errorf("empty catalog must show the empty state")
	}
	m = press(t, m, key("enter"))
	if !m.Done() || m.SubmitLine() != "" {
		t.Errorf("Enter on an empty menu must close with nothing to submit")
	}
}

func TestLoadErrorIsShownNotPanicked(t *testing.T) {
	t.Parallel()
	m, _ := New(context.Background(), fixedDeps{})
	next, _ := m.Update(loadMsg{err: errors.New("disk on fire")})
	if next.loading {
		t.Fatal("a load error must end the loading state")
	}
	if !strings.Contains(next.View(), "disk on fire") {
		t.Errorf("load error must be visible on the dialog")
	}
	next = press(t, next, key("enter"))
	if next.Done() {
		t.Errorf("Enter after a failed load must not submit or close silently")
	}
}

func TestEnterWhileLoadingDoesNothing(t *testing.T) {
	t.Parallel()
	m, _ := New(context.Background(), fixedDeps{})
	m = press(t, m, key("enter"))
	if m.Done() || m.SubmitLine() != "" {
		t.Errorf("Enter before the catalog arrives must not submit")
	}
}

func TestCurrentMarkerAndLiveMarkerRender(t *testing.T) {
	t.Parallel()
	m := loadedModel(t, sampleCatalog())
	view := m.View()
	for _, want := range []string{"— current", "— open in another terminal", "#1", "Sessions", "Checkpoints"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestWindowKeepsCursorVisibleAndReportsOverflow(t *testing.T) {
	t.Parallel()
	var sessions []SessionEntry
	for i := 1; i <= 40; i++ {
		sessions = append(sessions, SessionEntry{Index: i, ID: "id-" + strings.Repeat("x", i%3)})
	}
	m := loadedModel(t, Catalog{Sessions: sessions})
	m = m.SetSize(100, 20) // visible rows = 20 - chromeRows, clamped to min
	m.cursor = 30

	start, end := m.window()
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("window [%d,%d) excludes cursor %d", start, end, m.cursor)
	}
	if start == 0 || end == len(m.rows) {
		t.Errorf("window [%d,%d) should scroll both ways for a 40-row list", start, end)
	}
	view := m.View()
	if !strings.Contains(view, "more") {
		t.Errorf("scrolled list must report hidden rows")
	}
}

func TestFormatAge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		then time.Time
		want string
	}{
		{name: "zero is unknown", then: time.Time{}, want: ""},
		{name: "under a minute", then: testNow.Add(-30 * time.Second), want: "just now"},
		{name: "minutes", then: testNow.Add(-5 * time.Minute), want: "5 min ago"},
		{name: "hours", then: testNow.Add(-3 * time.Hour), want: "3 h ago"},
		{name: "days", then: testNow.Add(-50 * time.Hour), want: "2 d ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := formatAge(tc.then, testNow); got != tc.want {
				t.Errorf("formatAge = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTruncateRunesKeepsMultibyteIntact(t *testing.T) {
	t.Parallel()
	got := truncateRunes("héllo wörld, this is long", 8)
	if got != "héllo w…" {
		t.Errorf("truncateRunes = %q, want %q", got, "héllo w…")
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated title must end with an ellipsis")
	}
	if short := truncateRunes("ok", 8); short != "ok" {
		t.Errorf("short title changed to %q", short)
	}
}
