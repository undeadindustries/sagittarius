package bubbletea

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/undeadindustries/sagittarius/internal/diff"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// toolPhase is the lifecycle state of a single tool invocation, driving the
// card's status icon, border color, and body content.
type toolPhase int

const (
	// toolRunning is an in-flight invocation (spinner in the header; live output
	// or a "Running…" placeholder in the body).
	toolRunning toolPhase = iota
	// toolConfirming is awaiting the user's approval (nested command/diff preview
	// plus the numbered allow menu).
	toolConfirming
	// toolAsking is awaiting the user's answer to a grill-mode ask_user
	// question (the question text plus the numbered option menu, with an
	// automatic "Other" entry for free-text answers).
	toolAsking
	// toolSuccess is a completed invocation (result snippet / diff / exit code).
	toolSuccess
	// toolError is a failed, denied, or boundary-blocked invocation.
	toolError
)

// toolCard is the scrollback representation of one tool invocation. The same
// card is updated in place across its StreamToolStart → Output/Confirm →
// Result lifecycle, keyed by callID, so the user sees one grouped box rather
// than separate prefix lines.
type toolCard struct {
	callID      string
	toolName    string // wire name (e.g. run_shell_command, mcp_srv_tool)
	displayName string // human label (Shell, Write file, MCP tool name)
	serverName  string // MCP server name; empty for built-ins
	summary     string // truncated argument detail for the header
	body        string // live output / result / error / confirm preview text
	diff        string // unified-diff preview (write_file confirm + result)
	phase       toolPhase
	exitCode    *int

	// askQuestion/askOptions/askRecommended hold a pending grill-mode ask_user
	// prompt (toolAsking phase); askOptions never includes the automatic
	// "Other" entry, which the renderer appends.
	askQuestion    string
	askOptions     []ui.AskOption
	askRecommended int
}

// toolCardMaxBodyLines caps the live-output / result body height inside a card
// so a long-running command or large result does not dominate the viewport.
// Matches gemini-cli's ACTIVE_SHELL_MAX_LINES feel.
const toolCardMaxBodyLines = 15

// Wire names are duplicated here (rather than importing internal/tools) to keep
// the Bubble Tea layer free of a dependency on the tools package, mirroring how
// internal/tools itself duplicates the "mcp_" prefix to stay decoupled.
const (
	wireShell        = "run_shell_command"
	wireWriteFile    = "write_file"
	wireReadFile     = "read_file"
	wireListDir      = "list_directory"
	wireGrep         = "grep_search"
	wireChecks       = "run_project_checks"
	wireWebSearch    = "google_web_search"
	wireWebFetch     = "web_fetch"
	wireMCPPrefix    = "mcp_"
	wireMCPSeparator = "_"
)

// toolDisplayName maps a wire tool name to a short human label. MCP tools render
// as their bare tool segment; unknown built-ins fall back to the wire name.
func toolDisplayName(name string) string {
	if _, tool, ok := parseMCPToolName(name); ok {
		return tool
	}
	switch name {
	case wireShell, "shell", "run_shell":
		return "Shell"
	case wireWriteFile:
		return "Write file"
	case wireReadFile:
		return "Read file"
	case wireListDir:
		return "List directory"
	case wireGrep, "grep", "search_file_content":
		return "Search"
	case wireChecks:
		return "Project checks"
	case wireWebSearch:
		return "Web search"
	case wireWebFetch:
		return "Web fetch"
	case wireAskUser:
		return "Question"
	default:
		return name
	}
}

// wireAskUser is grill mode's structured-question tool (tools.AskUserToolName,
// duplicated here per the package's existing wire-name decoupling convention).
const wireAskUser = "ask_user"

// parseMCPToolName splits a qualified MCP wire name (mcp_{server}_{tool}) into
// its server and tool segments. The first underscore after the prefix separates
// the server from the (possibly underscore-containing) tool name.
func parseMCPToolName(name string) (server, tool string, ok bool) {
	if !strings.HasPrefix(name, wireMCPPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, wireMCPPrefix)
	idx := strings.Index(rest, wireMCPSeparator)
	if idx <= 0 || idx >= len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}

// newToolCard builds a running card from a StreamToolStart event.
func newToolCard(ev ui.StreamEvent) *toolCard {
	server, _, _ := parseMCPToolName(ev.ToolName)
	return &toolCard{
		callID:      ev.ToolCallID,
		toolName:    ev.ToolName,
		displayName: toolDisplayName(ev.ToolName),
		serverName:  server,
		summary:     ev.Text,
		phase:       toolRunning,
	}
}

// renderToolCard draws a rounded card for one tool invocation: a header row
// (status icon + display name + truncated summary) embedded in the top border,
// followed by phase-dependent body rows. Returns the box as a slice of lines so
// the scrollback renderer can splice it into the viewport content.
func (m *model) renderToolCard(c *toolCard, width int) []string {
	if width < 8 {
		width = 8
	}
	border := lipgloss.NewStyle().Foreground(m.toolCardBorderColor(c))
	inner := width - 4 // "│ " + content + " │"
	if inner < 1 {
		inner = 1
	}

	out := []string{m.toolCardTop(c, border, width)}
	for _, line := range m.toolCardBody(c, inner) {
		out = append(out, border.Render("│")+" "+padOrTruncate(line, inner)+" "+border.Render("│"))
	}
	out = append(out, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))
	return out
}

// toolCardBorderColor highlights a confirming card with the focus color so the
// pending decision draws the eye; other phases use the muted panel border.
func (m *model) toolCardBorderColor(c *toolCard) lipgloss.TerminalColor {
	if c.phase == toolConfirming || c.phase == toolAsking {
		return m.th.FocusBorderColor
	}
	return m.th.BorderColor
}

// toolCardTop builds the top border row with the status icon, display name, and
// a dim truncated summary embedded: "╭─ ✓ Shell  go build … ───╮".
func (m *model) toolCardTop(c *toolCard, border lipgloss.Style, width int) string {
	label := m.toolStatusGlyph(c) + " " + m.th.Accent.Bold(true).Render(c.displayName)
	// MCP tools carry a dim server badge so they are visually distinct from
	// built-ins (e.g. "✓ search  (context7) …").
	if c.serverName != "" {
		label += " " + m.th.Code.Render("("+c.serverName+")")
	}
	// Reserve "╭─ " (3) + label + " " (1) + "╮" (1) = 5 fixed columns.
	if avail := width - 5 - lipgloss.Width(label) - 2; c.summary != "" && avail > 3 {
		label += "  " + m.th.Dim.Render(truncateVisible(c.summary, avail))
	}
	fill := width - 5 - lipgloss.Width(label)
	if fill < 0 {
		fill = 0
	}
	return border.Render("╭─") + " " + label + " " + border.Render(strings.Repeat("─", fill)+"╮")
}

// toolStatusGlyph returns the styled one-column status indicator for the card's
// phase: an animated spinner while running, else a static icon.
func (m *model) toolStatusGlyph(c *toolCard) string {
	switch c.phase {
	case toolConfirming, toolAsking:
		return m.th.Warning.Render("?")
	case toolSuccess:
		return m.th.Success.Render("✓")
	case toolError:
		return m.th.Error.Render("✗")
	default:
		return m.spin.View()
	}
}

// toolCardBody returns the inner body lines (already wrapped/truncated to inner
// width) for the card's current phase.
func (m *model) toolCardBody(c *toolCard, inner int) []string {
	switch c.phase {
	case toolConfirming:
		return m.toolConfirmBody(c, inner)
	case toolAsking:
		return m.toolAskBody(c, inner)
	case toolError:
		return m.wrapStyled(strings.TrimSpace(c.body), inner, m.th.Error)
	default:
		return m.toolResultBody(c, inner)
	}
}

// toolResultBody renders the running/success body: a colorized diff when the
// text is a unified diff, otherwise the (capped) output, plus an exit-code
// footer for shell commands.
func (m *model) toolResultBody(c *toolCard, inner int) []string {
	text := strings.TrimSpace(c.body)
	if text == "" {
		if c.phase == toolRunning {
			return []string{m.th.Dim.Render("Running…")}
		}
		text = "ok"
	}

	var lines []string
	if diff.LooksLikeUnifiedDiff(text) {
		lines = m.renderDiffLines(text, inner, toolCardMaxBodyLines)
	} else {
		lines = m.wrapStyled(text, inner, m.th.Secondary)
		if len(lines) > toolCardMaxBodyLines {
			hidden := len(lines) - toolCardMaxBodyLines
			lines = append([]string{m.th.Dim.Render(fmt.Sprintf("… %d more lines", hidden))}, lines[len(lines)-toolCardMaxBodyLines:]...)
		}
	}
	if c.exitCode != nil {
		style := m.th.Success
		if *c.exitCode != 0 {
			style = m.th.Error
		}
		lines = append(lines, style.Render(fmt.Sprintf("exit %d", *c.exitCode)))
	}
	return lines
}

// toolConfirmBody renders the confirmation UX inside the card: a nested
// command/diff preview, the question, and the numbered allow menu with the
// current selection marked.
func (m *model) toolConfirmBody(c *toolCard, inner int) []string {
	var lines []string

	// Nested preview: the diff for write_file, otherwise the command summary.
	if c.diff != "" {
		for _, ln := range m.renderDiffLines(c.diff, max(inner-2, 1), confirmDiffMaxLines) {
			lines = append(lines, m.th.Dim.Render("▏ ")+ln)
		}
	} else if preview := strings.TrimSpace(c.body); preview != "" {
		for _, ln := range strings.Split(wrapText(preview, max(inner-2, 1)), "\n") {
			lines = append(lines, m.th.Dim.Render("▏ ")+m.th.Code.Render(ln))
		}
	}

	lines = append(lines, m.th.Warning.Render(fmt.Sprintf("Allow %s?", c.displayName)))
	for i, choice := range confirmChoices {
		row := fmt.Sprintf("%d %s", i+1, choice)
		if i == m.confirmChoice {
			lines = append(lines, m.th.Selected.Render("› "+row))
		} else {
			lines = append(lines, "  "+m.th.Secondary.Render(row))
		}
	}
	return lines
}

// toolAskBody renders a pending grill-mode ask_user question inside the card:
// the question text, the recommended-first numbered options, and an automatic
// trailing "Other" row for a free-text answer. Once "Other" is selected the
// card shows a hint to type the answer in the main input box instead.
func (m *model) toolAskBody(c *toolCard, inner int) []string {
	var lines []string
	for _, ln := range strings.Split(wrapText(strings.TrimSpace(c.askQuestion), max(inner, 1)), "\n") {
		lines = append(lines, m.th.Warning.Render(ln))
	}
	for i, opt := range c.askOptions {
		row := fmt.Sprintf("%d %s", i+1, opt.Label)
		if i == c.askRecommended {
			row += " " + m.th.Dim.Render("(recommended)")
		}
		if opt.Description != "" {
			row += "  " + m.th.Dim.Render(opt.Description)
		}
		if i == m.askChoice && !m.askOtherMode {
			lines = append(lines, m.th.Selected.Render("› "+row))
		} else {
			lines = append(lines, "  "+m.th.Secondary.Render(row))
		}
	}
	otherRow := fmt.Sprintf("%d Other — type my own", len(c.askOptions)+1)
	if m.askOtherMode || m.askChoice == len(c.askOptions) {
		lines = append(lines, m.th.Selected.Render("› "+otherRow))
	} else {
		lines = append(lines, "  "+m.th.Secondary.Render(otherRow))
	}
	if m.askOtherMode {
		lines = append(lines, m.th.Dim.Render("Type your answer below and press Enter."))
	}
	return lines
}

// wrapStyled wraps text to width and applies style to each resulting line.
func (m *model) wrapStyled(text string, width int, style lipgloss.Style) []string {
	var out []string
	for _, line := range strings.Split(wrapText(text, max(width, 1)), "\n") {
		out = append(out, style.Render(line))
	}
	return out
}
