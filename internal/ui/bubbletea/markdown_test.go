package bubbletea

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

func TestMarkdownHeadingAndBullets(t *testing.T) {
	md := "# Title\n\n- first\n- second\n"
	lines := renderMarkdown(md, 80, theme.Greyscale())
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Title") {
		t.Errorf("heading text missing:\n%s", joined)
	}
	if strings.Contains(joined, "# Title") {
		t.Errorf("heading marker should be stripped:\n%s", joined)
	}
	if !strings.Contains(joined, "• first") || !strings.Contains(joined, "• second") {
		t.Errorf("bullets not converted:\n%s", joined)
	}
}

func TestMarkdownFencedCode(t *testing.T) {
	md := "before\n```\ncode_line()\n```\nafter"
	lines := renderMarkdown(md, 80, theme.Greyscale())
	joined := stripANSI(strings.Join(lines, "\n"))
	if strings.Contains(joined, "```") {
		t.Errorf("code fences should be hidden:\n%s", joined)
	}
	if !strings.Contains(joined, "│ code_line()") {
		t.Errorf("code line missing left bar:\n%s", joined)
	}
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "after") {
		t.Errorf("prose around code missing:\n%s", joined)
	}
}

func TestMarkdownInlineStyling(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	out := strings.Join(renderMarkdown("a **bold** and `code` here", 80, theme.Default()), "\n")
	// Styling must wrap the words (ANSI present) but the words remain in the text.
	plain := stripANSI(out)
	if !strings.Contains(plain, "bold") || !strings.Contains(plain, "code") {
		t.Errorf("inline words lost:\n%s", plain)
	}
	if strings.Contains(plain, "**") || strings.Contains(plain, "`") {
		t.Errorf("inline markers should be consumed:\n%s", plain)
	}
	if out == plain {
		t.Error("expected ANSI styling on default theme inline markup")
	}
}

var mdAnsiColor = regexp.MustCompile(`\x1b\[[0-9;]*(?:38|48|3[0-9]|4[0-9])[;m]`)

func TestMarkdownGreyscaleNoColor(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	md := "# Heading\n**bold** `code`\n```\nx := 1\n```"
	out := strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n")
	if mdAnsiColor.MatchString(out) {
		t.Errorf("greyscale markdown emitted color codes:\n%q", out)
	}
}
