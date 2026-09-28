package bubbletea

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestToolDisplayName(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"run_shell_command":  "Shell",
		"write_file":         "Write file",
		"read_file":          "Read file",
		"grep_search":        "Search",
		"mcp_context7_query": "query",
		"unknown_tool":       "unknown_tool",
	}
	for in, want := range cases {
		if got := toolDisplayName(in); got != want {
			t.Errorf("toolDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseMCPToolName(t *testing.T) {
	t.Parallel()
	server, tool, ok := parseMCPToolName("mcp_context7_resolve_library")
	if !ok || server != "context7" || tool != "resolve_library" {
		t.Fatalf("parse = (%q, %q, %v)", server, tool, ok)
	}
	if _, _, ok := parseMCPToolName("write_file"); ok {
		t.Fatal("non-MCP name should not parse")
	}
}

func renderCard(m *model, c *toolCard) string {
	return stripANSI(strings.Join(m.renderToolCard(c, 80), "\n"))
}

func TestRenderToolCardSuccess(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	code := 0
	c := &toolCard{
		toolName:    wireShell,
		displayName: "Shell",
		summary:     "go build ./...",
		body:        "build ok",
		phase:       toolSuccess,
		exitCode:    &code,
	}
	out := renderCard(m, c)
	for _, want := range []string{"✓", "Shell", "go build ./...", "build ok", "exit 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("success card missing %q:\n%s", want, out)
		}
	}
}

func TestRenderToolCardError(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	c := &toolCard{
		toolName:    wireShell,
		displayName: "Shell",
		summary:     "ls /nope",
		body:        "No such file or directory",
		phase:       toolError,
	}
	out := renderCard(m, c)
	if !strings.Contains(out, "✗") || !strings.Contains(out, "No such file or directory") {
		t.Fatalf("error card missing icon/body:\n%s", out)
	}
}

func TestRenderToolCardConfirmMenu(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.confirmChoice = 1
	c := &toolCard{
		toolName:    wireShell,
		displayName: "Shell",
		summary:     "rm -rf build",
		body:        "run rm -rf build",
		phase:       toolConfirming,
	}
	out := renderCard(m, c)
	for _, want := range []string{"?", "Allow Shell?", "1 Allow once", "2 Allow for this session", "3 No"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirm card missing %q:\n%s", want, out)
		}
	}
	// The selected row (index 1) is marked.
	if !strings.Contains(out, "› 2 Allow for this session") {
		t.Fatalf("confirm card should mark the selected row:\n%s", out)
	}
}

// cancelApp records CancelSubagent calls for the per-child cancel tests
// (AD-154).
type cancelApp struct {
	quitApp
	got []string
	ret bool
}

func (c *cancelApp) CancelSubagent(id string) bool {
	c.got = append(c.got, id)
	return c.ret
}

// TestTaskGroupCtrlXCancelsSelectedChild: Ctrl+X cancels only the selected,
// still-running card; a finished sibling and an accidental press with no
// selection both do nothing. Review and fix cards in the task group can be
// canceled the same way.
func TestTaskGroupCtrlXCancelsSelectedChild(t *testing.T) {
	t.Parallel()
	app := &cancelApp{ret: true}
	m := newTestModel()
	m.app = app

	cards := []*toolCard{
		{callID: "c1", toolName: "code_task", displayName: "Coding subagent", summary: "A", phase: toolRunning},
		{callID: "c2", toolName: "code_task", displayName: "Coding subagent", summary: "B", phase: toolSuccess},
		{callID: "c1#review-1", toolName: "batch_review", displayName: "Review", summary: "Batch review", phase: toolRunning},
	}
	m.blocks = append(m.blocks, scrollBlock{role: roleTaskGroup, taskGroup: &taskGroupBlock{tasks: cards, selectedIdx: 0}})

	if !m.handleTaskGroupKey("ctrl+x") {
		t.Fatal("ctrl+x must be swallowed by the task group")
	}
	if len(app.got) != 1 || app.got[0] != "c1" {
		t.Fatalf("CancelSubagent calls = %v, want [c1]", app.got)
	}
	if cards[0].body != "Canceling…" {
		t.Errorf("card body = %q, want the cancel acknowledgment", cards[0].body)
	}

	// The review card can be canceled when selected.
	m.lastTaskGroup().selectedIdx = 2
	m.handleTaskGroupKey("ctrl+x")
	if len(app.got) != 2 || app.got[1] != "c1#review-1" {
		t.Fatalf("CancelSubagent calls = %v, want second call [c1#review-1]", app.got)
	}
	if cards[2].body != "Canceling…" {
		t.Errorf("card body = %q, want the cancel acknowledgment", cards[2].body)
	}

	// The selected card is finished: nothing to cancel.
	m.lastTaskGroup().selectedIdx = 1
	m.handleTaskGroupKey("ctrl+x")
	if len(app.got) != 2 {
		t.Errorf("a finished card must not be canceled: %v", app.got)
	}

	// No selection: an accidental press does nothing.
	m.lastTaskGroup().selectedIdx = -1
	m.handleTaskGroupKey("ctrl+x")
	if len(app.got) != 2 {
		t.Errorf("no selection must not cancel: %v", app.got)
	}
}

// TestStartToolCardGroupsBatchReviewAndFix verifies that batch_review and
// batch_fix start events are appended to the subagent task group.
func TestStartToolCardGroupsBatchReviewAndFix(t *testing.T) {
	t.Parallel()
	m := newTestModel()

	// Initial code_task creates the task group.
	m.startToolCard(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "code_task", ToolCallID: "c1", Text: "Task A"})
	if len(m.blocks) != 1 || m.blocks[0].role != roleTaskGroup {
		t.Fatalf("expected 1 task group block, got %+v", m.blocks)
	}
	tg := m.blocks[0].taskGroup
	if len(tg.tasks) != 1 || tg.tasks[0].callID != "c1" {
		t.Fatalf("expected 1 task c1, got %+v", tg.tasks)
	}

	// batch_review appends to the same task group.
	m.startToolCard(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "batch_review", ToolCallID: "c1#review-1", Text: "Batch review"})
	if len(m.blocks) != 1 {
		t.Fatalf("expected batch_review to stay in task group, got %d blocks", len(m.blocks))
	}
	if len(tg.tasks) != 2 || tg.tasks[1].callID != "c1#review-1" || tg.tasks[1].displayName != "Review" {
		t.Fatalf("expected task 2 to be review, got %+v", tg.tasks)
	}

	// batch_fix appends to the same task group.
	m.startToolCard(ui.StreamEvent{Type: ui.StreamToolStart, ToolName: "batch_fix", ToolCallID: "c1#fix-1", Text: "Fix review findings"})
	if len(m.blocks) != 1 {
		t.Fatalf("expected batch_fix to stay in task group, got %d blocks", len(m.blocks))
	}
	if len(tg.tasks) != 3 || tg.tasks[2].callID != "c1#fix-1" || tg.tasks[2].displayName != "Fix" {
		t.Fatalf("expected task 3 to be fix, got %+v", tg.tasks)
	}
}

// TestTaskGroupCancelHint renders the Ctrl+X hint only when a running card is
// selected.
func TestTaskGroupCancelHint(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	cards := []*toolCard{
		{callID: "c1", toolName: "code_task", displayName: "Coding subagent", summary: "A", phase: toolRunning},
	}
	tg := &taskGroupBlock{tasks: cards, selectedIdx: 0}
	if out := stripANSI(strings.Join(m.renderTaskGroup(tg, 60), "\n")); !strings.Contains(out, "Ctrl+X cancel") {
		t.Errorf("selected running card must show the cancel hint:\n%s", out)
	}
	tg.selectedIdx = -1
	if out := stripANSI(strings.Join(m.renderTaskGroup(tg, 60), "\n")); strings.Contains(out, "Ctrl+X cancel") {
		t.Errorf("no selection must not show the hint:\n%s", out)
	}
	cards[0].phase = toolSuccess
	tg.selectedIdx = 0
	if out := stripANSI(strings.Join(m.renderTaskGroup(tg, 60), "\n")); strings.Contains(out, "Ctrl+X cancel") {
		t.Errorf("a finished card must not show the hint:\n%s", out)
	}
}

// TestRenderTaskGroupIntegrity pins the benchmark failure: a completed
// non-selected task's body is a multi-line result, and rendering it raw leaked
// the embedded newlines and broke the frame. The group now shows a titled
// frame, a per-row routing badge, and a one-line activity.
func TestRenderTaskGroupIntegrity(t *testing.T) {
	t.Parallel()
	m := newTestModel()

	body := "via qwen/qwen3.8-27b\nWrote 4 file(s): app/db/__init__.py, app/db/connection.py, app/db/repository.py, app/db/schema.py\nDone."
	cards := []*toolCard{
		{toolName: "code_task", displayName: "Coding subagent", badge: "qwen/qwen3.8-27b", summary: "Implement app/db layer", body: body, phase: toolSuccess},
		{toolName: "code_task", displayName: "Coding subagent", badge: "qwen/qwen3.8-27b", summary: "Implement app/models", body: body, phase: toolSuccess},
	}
	tg := &taskGroupBlock{tasks: cards, selectedIdx: 0}

	for _, width := range []int{84, 60, 40} {
		lines := m.renderTaskGroup(tg, width)
		for i, ln := range lines {
			if w := lipgloss.Width(ln); w != width {
				t.Errorf("width %d line %d width = %d, want %d:\n%q", width, i, w, width, stripANSI(ln))
			}
			plain := stripANSI(ln)
			if !strings.HasPrefix(plain, "╭") && !strings.HasPrefix(plain, "│") && !strings.HasPrefix(plain, "╰") {
				t.Errorf("width %d line %d missing left border: %q", width, i, plain)
			}
		}
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, "Sub Agents") {
			t.Errorf("width %d: group frame missing its title", width)
		}
		// The badge survives truncation at wide-enough widths; at 40 the
		// description wins the truncation and the badge may be cut.
		if width >= 60 && !strings.Contains(joined, "(qwen/qwen3.8-27b)") {
			t.Errorf("width %d: group rows missing the routing badge", width)
		}
	}
}

// TestRenderTaskGroupSingleLineActivity pins the specific break: a
// non-selected task's activity must be one line even when its result body is
// multi-line. The selected task's body is exempt — it is expanded by design.
func TestRenderTaskGroupSingleLineActivity(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	cards := []*toolCard{
		{toolName: "code_task", displayName: "Coding subagent", summary: "A", body: "selected full body", phase: toolSuccess},
		{toolName: "code_task", displayName: "Coding subagent", summary: "B", body: "firstactivity\nsecondactivity", phase: toolSuccess},
	}
	tg := &taskGroupBlock{tasks: cards, selectedIdx: 0}
	lines := m.renderTaskGroup(tg, 60)
	joined := strings.Join(lines, "\n")
	// Task B is not selected: only its first body line may appear.
	if strings.Contains(joined, "secondactivity") {
		t.Errorf("non-selected task leaked a second body line into the frame:\n%s", joined)
	}
	if !strings.Contains(joined, "firstactivity") {
		t.Errorf("non-selected task missing its one-line activity:\n%s", joined)
	}
}

func TestRenderToolCardMCPBadge(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	c := &toolCard{
		toolName:    "mcp_context7_query",
		displayName: "query",
		serverName:  "context7",
		body:        "result text",
		phase:       toolSuccess,
	}
	out := renderCard(m, c)
	if !strings.Contains(out, "query") || !strings.Contains(out, "(context7)") {
		t.Fatalf("MCP card missing tool/server badge:\n%s", out)
	}
}

func TestRenderToolCardSubagentBadge(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	c := &toolCard{
		toolName:    "code_task",
		displayName: "Coding subagent",
		badge:       "local/qwen3.8-27b",
		body:        "result text",
		phase:       toolSuccess,
	}
	out := renderCard(m, c)
	if !strings.Contains(out, "Coding subagent") || !strings.Contains(out, "(local/qwen3.8-27b)") {
		t.Fatalf("subagent card missing routing badge:\n%s", out)
	}
}

// TestNewToolCardReadsBadge pins the event seam: StreamToolStart.Badge lands
// on the card at creation.
func TestNewToolCardReadsBadge(t *testing.T) {
	t.Parallel()
	c := newToolCard(ui.StreamEvent{
		Type:       ui.StreamToolStart,
		ToolName:   "task",
		ToolCallID: "c1",
		Text:       "research something",
		Badge:      "openrouter/qwen/qwen3-coder",
	})
	if c.badge != "openrouter/qwen/qwen3-coder" {
		t.Fatalf("card badge = %q, want the event's badge", c.badge)
	}
	if c.displayName != "Research subagent" {
		t.Fatalf("displayName = %q", c.displayName)
	}
}

func TestRenderToolCardWidthInvariants(t *testing.T) {
	t.Parallel()
	m := newTestModel()

	const width = 40
	code := 1

	// Complex card combining all the edge cases that trigger soft-wrap
	c := &toolCard{
		toolName:    wireShell,
		displayName: "Shell",
		// CJK characters to stress runewidth vs byte len
		summary: "运行一些带有中文的命令 to test truncateVisible",
		// Tabs, CR, and ANSI to stress padOrTruncate and cell wrapping
		body:     "\x1b[31mThis line has a tab\there and a\r\ncarriage return\rthat should be sanitized.\n\x1b[0m",
		phase:    toolSuccess,
		exitCode: &code,
	}

	lines := m.renderToolCard(c, width)
	for i, line := range lines {
		// lipgloss.Width correctly handles ANSI
		gotWidth := lipgloss.Width(line)

		// The rendered card line width must not exceed the target terminal width,
		// otherwise the terminal will soft-wrap and break the UI.
		if gotWidth > width {
			t.Errorf("card line %d exceeds width: got %d, max %d\nline text: %q", i, gotWidth, width, line)
		}
	}
}

func TestRenderToolCardWideDiffWrapsInsideBorder(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	const width = 40
	diffBody := "--- a/x\n+++ b/x\n@@ -1,1 +1,1 @@\n+" + strings.Repeat("    indented_token ", 12) + "\n"
	c := &toolCard{
		toolName:    "write_file",
		displayName: "Write file",
		body:        diffBody,
		phase:       toolSuccess,
	}
	lines := m.renderToolCard(c, width)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "indented_token") {
		t.Errorf("diff content missing after wrap:\n%s", joined)
	}
	if !strings.Contains(joined, "+    indented_token") {
		t.Errorf("first wrapped diff row lost indent:\n%s", joined)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("card line %d exceeds width: got %d, max %d\nline text: %q", i, got, width, line)
		}
	}
}

func TestStreamToolConfirmWithoutPriorStartCreatesCard(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := make(chan ui.ConfirmDecision, 1)

	// StreamToolConfirm arrives for continue_agent without prior StreamToolStart
	m.handleStream(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     wireContinueAgent,
		Text:         "Max tool rounds reached (100). Continue for another 100 rounds?",
		ConfirmReply: reply,
	})

	if m.activeCard == nil {
		t.Fatal("expected activeCard to be created for StreamToolConfirm")
	}
	if m.activeCard.phase != toolConfirming {
		t.Errorf("card phase = %v, want toolConfirming", m.activeCard.phase)
	}
	if m.activeCard.displayName != "Continue" {
		t.Errorf("displayName = %q, want Continue", m.activeCard.displayName)
	}
	if m.confirmReply == nil {
		t.Fatal("expected confirmReply to be set")
	}

	out := renderCard(m, m.activeCard)
	if !strings.Contains(out, "Max tool rounds reached (100)") {
		t.Fatalf("rendered card missing prompt text:\n%s", out)
	}
	if !strings.Contains(out, "1 Allow once") {
		t.Fatalf("rendered card missing menu options:\n%s", out)
	}

	// Pressing '1' should reply with ConfirmOnce and settle the card (this
	// confirm has no later Start/Result, so it must not stick on Running…).
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	select {
	case d := <-reply:
		if d != ui.ConfirmOnce {
			t.Errorf("decision = %v, want ConfirmOnce", d)
		}
	default:
		t.Fatal("no decision delivered to confirmReply channel")
	}

	if m.confirmReply != nil {
		t.Error("expected confirmReply to be cleared after sending decision")
	}
	if m.activeCard.phase != toolSuccess {
		t.Errorf("card phase = %v, want toolSuccess", m.activeCard.phase)
	}
	wantBody := "Continuing for another 100 rounds."
	if m.activeCard.body != wantBody {
		t.Errorf("card body = %q, want %q", m.activeCard.body, wantBody)
	}
	settled := renderCard(m, m.activeCard)
	if strings.Contains(settled, "Running…") {
		t.Fatalf("settled Continue card still shows Running…:\n%s", settled)
	}
	if !strings.Contains(settled, wantBody) {
		t.Fatalf("settled Continue card missing result body:\n%s", settled)
	}
}

func TestStreamToolConfirmContinueDenySettlesError(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := make(chan ui.ConfirmDecision, 1)

	m.handleStream(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     wireContinueAgent,
		Text:         "Max tool rounds reached (100). Continue for another 100 rounds?",
		ConfirmReply: reply,
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	select {
	case d := <-reply:
		if d != ui.ConfirmDeny {
			t.Errorf("decision = %v, want ConfirmDeny", d)
		}
	default:
		t.Fatal("no decision delivered to confirmReply channel")
	}
	if m.activeCard.phase != toolError {
		t.Errorf("card phase = %v, want toolError", m.activeCard.phase)
	}
	if m.activeCard.body != continueStoppedBody {
		t.Errorf("card body = %q, want %q", m.activeCard.body, continueStoppedBody)
	}
}

func TestStreamToolConfirmContinueSessionSettles(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := make(chan ui.ConfirmDecision, 1)

	m.handleStream(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     wireContinueAgent,
		Text:         "Max tool rounds reached (50). Continue for another 50 rounds?",
		ConfirmReply: reply,
	})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	select {
	case d := <-reply:
		if d != ui.ConfirmSession {
			t.Errorf("decision = %v, want ConfirmSession", d)
		}
	default:
		t.Fatal("no decision delivered to confirmReply channel")
	}
	if m.activeCard.phase != toolSuccess {
		t.Errorf("card phase = %v, want toolSuccess", m.activeCard.phase)
	}
	if m.activeCard.body != continueSessionBody {
		t.Errorf("card body = %q, want %q", m.activeCard.body, continueSessionBody)
	}
}
