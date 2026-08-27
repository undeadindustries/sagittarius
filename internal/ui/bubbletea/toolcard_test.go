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

func TestStreamToolConfirmWithoutPriorStartCreatesCard(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	reply := make(chan ui.ConfirmDecision, 1)

	// StreamToolConfirm arrives for continue_agent without prior StreamToolStart
	m.handleStream(ui.StreamEvent{
		Type:         ui.StreamToolConfirm,
		ToolName:     "continue_agent",
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

	// Pressing '1' should reply with ConfirmOnce
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
}
