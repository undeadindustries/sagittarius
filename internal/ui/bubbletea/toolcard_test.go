package bubbletea

import (
	"strings"
	"testing"
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
