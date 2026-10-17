package bubbletea

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

type statusApp struct {
	cs ui.ComposerStatus
}

func (statusApp) HandleInput(context.Context, string) (<-chan ui.StreamEvent, error) {
	ch := make(chan ui.StreamEvent)
	close(ch)
	return ch, nil
}

func (a statusApp) ComposerStatus() ui.ComposerStatus { return a.cs }

func TestApprovalHint(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"default":  "Tools: confirm before run",
		"":         "Tools: confirm before run",
		"autoEdit": "Tools: auto-accept edits",
		"yolo":     "Tools: auto-approve all (yolo)",
	}
	for in, want := range cases {
		if got := approvalHint(in); got != want {
			t.Errorf("approvalHint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContextSummaryOmitsZero(t *testing.T) {
	t.Parallel()
	if got := contextSummary(0, 0); got != "" {
		t.Fatalf("empty summary = %q, want empty", got)
	}
	if got := contextSummary(1, 0); got != "1 AGENTS.md file" {
		t.Fatalf("summary = %q", got)
	}
	if got := contextSummary(2, 3); got != "2 AGENTS.md files · 3 skills" {
		t.Fatalf("summary = %q", got)
	}
	if got := contextSummary(0, 1); got != "1 skill" {
		t.Fatalf("summary = %q", got)
	}
}

func TestRenderStatusRowShowsHintsAndCountsNotPolicy(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale", LoadedMemoryFiles: []string{"/a/AGENTS.md", "/b/AGENTS.md"}}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "yolo", SkillCount: 4}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	// Wide terminal so the full composed row fits; clamping at narrow widths is
	// covered by TestStatusRowNeverExceedsWidth.
	m.width = 150
	m.height = 24

	if m.statusRowRows() != 1 {
		t.Fatalf("statusRowRows = %d, want 1", m.statusRowRows())
	}
	row := stripANSI(m.renderStatusRow())
	hints := m.scrollShortcutHints()
	for _, want := range []string{hints, "2 AGENTS.md files", "4 skills"} {
		if !strings.Contains(row, want) {
			t.Errorf("status row missing %q\n%s", want, row)
		}
	}
	// The tool policy is a setting, so it lives in the footer, not here.
	for _, banned := range []string{"Tools:", "yolo"} {
		if strings.Contains(row, banned) {
			t.Errorf("status row should not show setting %q\n%s", banned, row)
		}
	}
}

func TestToolPolicyMovesToFooterSettings(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cs   ui.ComposerStatus
		want string
	}{
		{"yolo", ui.ComposerStatus{ApprovalMode: "yolo"}, "Tools: auto-approve all (yolo) · screen: full"},
		{"default", ui.ComposerStatus{ApprovalMode: "default"}, "Tools: confirm before run · screen: full"},
		{"read-only wins", ui.ComposerStatus{ApprovalMode: "yolo", ReadOnlyPosture: true}, "Tools: read-only · screen: full"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newModel(ui.Options{ThemeName: "greyscale"}, statusApp{cs: tc.cs}, NewTerminal(ui.Options{}))
			if got := m.footerSettings(); !strings.HasPrefix(got, tc.want) {
				t.Errorf("footerSettings = %q, want prefix %q", got, tc.want)
			}
		})
	}
}

func TestReadOnlyLiftKeyStaysInStatusRow(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale"}
	m := newModel(opts, statusApp{cs: ui.ComposerStatus{ReadOnlyPosture: true}}, NewTerminal(ui.Options{}))
	left, _ := m.statusRowParts()
	if !strings.HasPrefix(left, readOnlyLiftHint+"  ·  ") {
		t.Errorf("status row left = %q, want it to lead with %q", left, readOnlyLiftHint)
	}
	if strings.Contains(left, "Tools:") {
		t.Errorf("status row repeats the posture label: %q", left)
	}

	m = newModel(opts, statusApp{}, NewTerminal(ui.Options{}))
	if left, _ := m.statusRowParts(); strings.Contains(left, readOnlyLiftHint) {
		t.Errorf("lift hint shown while read-only is off: %q", left)
	}
}

func TestRenderStatusRowShowsScrollHintsWithoutComposerStatus(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.width = 80
	row := stripANSI(m.renderStatusRow())
	hints := m.scrollShortcutHints()
	if !strings.Contains(row, hints) {
		t.Errorf("status row missing scroll hints %q\n%s", hints, row)
	}
	if m.statusRowRows() != 1 {
		t.Fatalf("statusRowRows = %d, want 1", m.statusRowRows())
	}
}

// TestStatusRowNeverExceedsWidth reproduces the ghosting bug: with the key
// hints plus a skill count, the status row's left hints + right counts
// exceed an 80-col terminal, soft-wrap, and leave duplicate footer/status rows.
// renderStatusRowLine must clamp to width.
func TestStatusRowNeverExceedsWidth(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale"}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "default", SkillCount: 1}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	for _, w := range []int{40, 60, 80, 100} {
		m.width = w
		if got := lipgloss.Width(m.renderStatusRow()); got > w {
			t.Errorf("status row width = %d at terminal width %d (overflows → ghosting)", got, w)
		}
	}
}

// TestTruncateAtSeparator keeps whole entries and never cuts one in half. The
// line is "screen: full · thinking: hidden · mouse: on" (12, 16, and 9 cells
// plus two 3-cell separators, 43 in all).
func TestTruncateAtSeparator(t *testing.T) {
	t.Parallel()
	const line = "screen: full · thinking: hidden · mouse: on"
	cases := []struct {
		name  string
		width int
		want  string
	}{
		{"fits", 80, line},
		{"exact fit", 43, line},
		{"drops last entry", 42, "screen: full · thinking: hidden"},
		{"cut lands on a boundary", 31, "screen: full · thinking: hidden"},
		{"cut lands inside the separator", 14, "screen: full"},
		{"cut lands inside the next entry", 20, "screen: full"},
		{"first entry only", 12, "screen: full"},
		{"first entry overflows", 6, "screen"},
		{"zero width keeps one cell", 0, "s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := truncateAtSeparator(line, tc.width)
			if got != tc.want {
				t.Errorf("truncateAtSeparator(%d) = %q, want %q", tc.width, got, tc.want)
			}
			if w := lipgloss.Width(got); w > max(tc.width, 1) {
				t.Errorf("width %d overflows %d", w, tc.width)
			}
		})
	}
}

// TestKeyAndSettingsRowsTruncateNotWrap checks every composer row at widths
// from tiny to wide: no row is wider than the terminal and none contains a
// newline, so nothing wraps.
func TestKeyAndSettingsRowsTruncateNotWrap(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "default", LoadedMemoryFiles: []string{"/a/AGENTS.md", "/b/AGENTS.md"}}
	app := footerMetricsApp{stats: ui.SessionStats{
		LastInputTokens:  1000,
		LastOutputTokens: 400,
		InputTokens:      100,
		OutputTokens:     40,
		TokensPerSec:     42,
		ContextTokens:    5000,
		ContextLimit:     10000,
	}}
	for _, inline := range []bool{false, true} {
		for _, w := range []int{1, 8, 20, 30, 45, 60, 80, 120} {
			m := newModel(opts, app, NewTerminal(ui.Options{}))
			m.inline = inline
			m.width = w
			m.status = ui.StatusBar{Left: "~/src/test", Right: "gemini - gemini-3.8-flash", Detail: "System Prompt: System administrator"}

			row := m.renderStatusRow()
			if strings.Contains(row, "\n") || lipgloss.Width(row) > w {
				t.Errorf("inline=%v w=%d status row wraps or overflows (%d): %q", inline, w, lipgloss.Width(row), stripANSI(row))
			}
			footer := renderFooter(m.statusWithMetrics(), m.th, w)
			for i, line := range strings.Split(footer, "\n") {
				if got := lipgloss.Width(line); got > w {
					t.Errorf("inline=%v w=%d footer line %d width %d overflows: %q", inline, w, i, got, stripANSI(line))
				}
			}
		}
	}
}

// TestFitLeftRightDropsWholeHints verifies the status row sheds trailing hints
// before it cuts a word.
func TestFitLeftRightDropsWholeHints(t *testing.T) {
	t.Parallel()
	left := readOnlyLiftHint + "  ·  " + scrollShortcutHintsForGOOS("linux")
	// 111 cells leaves 80 for the left side: through "show thinking" (69) fits,
	// the mouse hint would end at 88.
	gotLeft, _ := fitLeftRight(left, "2 AGENTS.md files · 119 skills", 111)
	want := "/readonly off to lift  ·  /screen display mode · Ctrl+T show thinking"
	if gotLeft != want {
		t.Errorf("fitted left = %q, want %q", gotLeft, want)
	}
}

// TestFitLeftRightKeepsRight verifies the truncation favors the right-aligned
// segment (live metrics) and never produces an over-width pair.
func TestFitLeftRightKeepsRight(t *testing.T) {
	t.Parallel()
	left := strings.Repeat("L", 70)
	right := "↑33.6k ↓1 100% ctx"
	gotLeft, gotRight := fitLeftRight(left, right, 80)
	if gotRight != right {
		t.Errorf("right segment was altered: %q", gotRight)
	}
	if total := lipgloss.Width(gotLeft) + lipgloss.Width(gotRight) + 1; total > 80 {
		t.Errorf("fitted parts still overflow: %d > 80", total)
	}
}

// TestViewNeverExceedsTerminalWidth is the end-to-end guard: a full frame with a
// realistic composer status must not emit any line wider than the terminal, so
// the renderer cannot ghost.
func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale", LoadedMemoryFiles: []string{"/a/AGENTS.md"}}
	app := statusApp{cs: ui.ComposerStatus{ApprovalMode: "default", SkillCount: 1}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	m.width = 80
	m.height = 24
	m.status = ui.StatusBar{
		Left:   "~/src/test",
		Right:  "openrouter - z-ai/glm-4.5-air",
		Detail: "System Prompt: Programmer",
	}
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("View line %d width = %d exceeds %d: %q", i, w, m.width, stripANSI(line))
		}
	}
}

// TestTitleAnnouncementLatchAndDismiss verifies the prompt-mode auto-title is
// latched from the app, persists across renders (peek, never consumed), and is
// hidden once dismissed.
func TestTitleAnnouncementLatchAndDismiss(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale"}
	app := statusApp{cs: ui.ComposerStatus{
		ApprovalMode:          "default",
		TitleAnnouncementText: `Named "Fix LSP pool race" — Ctrl+E rename`,
	}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	m.width = 120

	row := stripANSI(m.renderStatusRow())
	if !strings.Contains(row, `Named "Fix LSP pool race"`) {
		t.Fatalf("announcement not rendered\n%s", row)
	}
	// Second render must still show it (latched, not consumed on first read).
	row2 := stripANSI(m.renderStatusRow())
	if !strings.Contains(row2, `Named "Fix LSP pool race"`) {
		t.Fatalf("announcement not persisted across renders\n%s", row2)
	}
	// Dismiss: hidden from then on.
	m.titleDismissed = true
	row3 := stripANSI(m.renderStatusRow())
	if strings.Contains(row3, "Named") {
		t.Fatalf("announcement still shown after dismiss\n%s", row3)
	}
}

// TestTitleAnnouncementDismissedOnSubmit verifies submitting a real message
// clears the announcement so it does not linger as a stale status line.
func TestTitleAnnouncementDismissedOnSubmit(t *testing.T) {
	t.Parallel()
	opts := ui.Options{ThemeName: "greyscale"}
	app := statusApp{cs: ui.ComposerStatus{
		TitleAnnouncementText: `Named "X" — Ctrl+E rename`,
	}}
	m := newModel(opts, app, NewTerminal(ui.Options{}))
	m.width = 120
	m.input.SetValue("next question")

	_ = stripANSI(m.renderStatusRow()) // latch
	if m.titleAnnouncement == "" {
		t.Fatal("announcement not latched before submit")
	}
	m2, _ := m.handleEnter()
	mm := m2.(*model)
	if !mm.titleDismissed {
		t.Fatal("submitting a message did not dismiss the announcement")
	}
}

func TestLastResponseCopyHint(t *testing.T) {
	t.Parallel()
	const fenced = "```bash\necho hi\n```"
	const twoBlocks = "```\nfirst\n```\ntext\n```\nsecond\n```"
	tests := []struct {
		name   string
		blocks []scrollBlock
		want   string
	}{
		{name: "no blocks", blocks: nil, want: ""},
		{name: "plain reply", blocks: []scrollBlock{{role: roleResponse, text: "just prose"}}, want: "/copy reply"},
		{name: "one fenced block", blocks: []scrollBlock{{role: roleResponse, text: "try:\n" + fenced}}, want: "/copy reply · /copy code"},
		{name: "two fenced blocks", blocks: []scrollBlock{{role: roleResponse, text: twoBlocks}}, want: "/copy reply · /copy code 1-2"},
		{name: "empty reply", blocks: []scrollBlock{{role: roleResponse, text: "  "}}, want: ""},
		{
			name: "latest reply wins",
			blocks: []scrollBlock{
				{role: roleResponse, text: "old"},
				{role: roleInfo, text: "note"},
				{role: roleResponse, text: "new\n" + fenced},
			},
			want: "/copy reply · /copy code",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := lastResponseCopyHint(tc.blocks); got != tc.want {
				t.Errorf("lastResponseCopyHint = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCopyHintLifecycle drives a fenced reply through StreamDone and asserts
// the hint reaches the status row, then that submitting the next message
// clears it. The status row is outside the viewport, so a mouse selection of
// the reply never copies the hint (AD-163).
func TestCopyHintLifecycle(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.streamGen++
	m.activeStreamGen = m.streamGen
	const reply = "Run this:\n```bash\nsudo systemctl restart nginx\n```\n"
	updated, _ := m.Update(streamEventMsg{gen: m.activeStreamGen, event: ui.StreamEvent{Type: ui.StreamTextDelta, Text: reply}})
	m = updated.(*model)
	updated, _ = m.Update(streamEventMsg{gen: m.activeStreamGen, event: ui.StreamEvent{Type: ui.StreamDone}})
	m = updated.(*model)

	if m.copyHint != "/copy reply · /copy code" {
		t.Fatalf("copyHint = %q after fenced reply, want %q", m.copyHint, "/copy reply · /copy code")
	}
	if row := stripANSI(m.renderStatusRow()); !strings.Contains(row, "/copy reply · /copy code") {
		t.Errorf("status row missing copy hint:\n%s", row)
	}

	m.input.SetValue("next question")
	updated, _ = m.handleEnter()
	m = updated.(*model)
	if m.copyHint != "" {
		t.Errorf("copy hint not cleared on submit: %q", m.copyHint)
	}
}

func TestScrollShortcutHintsForGOOS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		goos string
		want string
	}{
		{"darwin", "/screen display mode · Ctrl+T show thinking · Alt+M or ⌥M mouse · Fn↑ Fn↓"},
		{"windows", "/screen display mode · Ctrl+T show thinking · Alt+M or ⌥M mouse · Pg↑ Pg↓"},
		{"linux", "/screen display mode · Ctrl+T show thinking · Alt+M or ⌥M mouse · Pg↑ Pg↓"},
		{"freebsd", "/screen display mode · Ctrl+T show thinking · Alt+M or ⌥M mouse · Pg↑ Pg↓"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.goos, func(t *testing.T) {
			t.Parallel()
			if got := scrollShortcutHintsForGOOS(tc.goos); got != tc.want {
				t.Fatalf("scrollShortcutHintsForGOOS(%q) = %q, want %q", tc.goos, got, tc.want)
			}
		})
	}
}
