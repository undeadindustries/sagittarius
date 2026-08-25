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
	if !strings.Contains(joined, "code_line()") {
		t.Errorf("code line missing:\n%s", joined)
	}
	if strings.Contains(joined, "│") {
		t.Errorf("fenced code must not paint a copyable gutter:\n%s", joined)
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
	md := "# Heading\n**bold** `code`\n```\nx := 1\n```\n| Col A | Col B |\n|---|---|\n| val1 | val2 |\n"
	out := strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n")
	if mdAnsiColor.MatchString(out) {
		t.Errorf("greyscale markdown emitted color codes:\n%q", out)
	}
}

func TestMarkdownTableReportedFixture(t *testing.T) {
	md := `Here is the exact comparison of the settings:

| Metric | Before (FP16 KV, 1 step, No DFlash) | Now (FP8 KV, steps=4, DFlash 2 Drafter) | Speedup / Impact |
|---|---|---|---|
| Single-User Pure Coding Speed | ~28 – 35 tok/s | ~65 – 82 tok/s | ~2.2× faster |
| 4 Concurrent Requests (Current Load) | ~12 – 14 tok/s aggregate (memory bus saturated) | ~32.7 tok/s aggregate | ~2.4× higher throughput |
| DFlash Speculative Acceptance (accept len) | 1.0 (1 token/pass) | ~2.61 tokens accepted per pass | Generates 2.6× more tokens per forward pass |
| KV Cache Memory Bandwidth Pressure | 100% FP16 bandwidth load | 50% reduced traffic (FP8) | Prevents UMA memory bus throttling |
| Prompt Digest Speed (Prefill) | ~300–600 tok/s | 1,066 to 52,000+ tok/s | Near-zero Time-to-First-Token |
`
	lines := renderMarkdown(md, 80, theme.Default())
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > 80 {
			t.Errorf("line %d width %d exceeds 80: %q", i, w, stripANSI(line))
		}
		if strings.Contains(line, "|---|") {
			t.Errorf("raw separator line should not be displayed: %q", line)
		}
	}
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Metric") || !strings.Contains(joined, "Single-User") {
		t.Errorf("table content missing from output:\n%s", joined)
	}
	for _, l := range lines {
		plain := strings.TrimSpace(stripANSI(l))
		if plain == "|" || plain == "• |" {
			t.Errorf("orphaned pipe line found:\n%s", plain)
		}
	}
}

func TestMarkdownTableAlignmentAndWrapping(t *testing.T) {
	md := `| Short | Very long description of the setting and its overall impact on memory |
|:---|---:|
| Alpha | First detailed explanation of the alpha parameter |
| Beta | Second detailed explanation of the beta parameter |
`
	lines := renderMarkdown(md, 40, theme.Default())
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w > 40 {
			t.Errorf("line %d width %d exceeds 40: %q", i, w, stripANSI(line))
		}
	}
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Alpha") || !strings.Contains(joined, "Beta") {
		t.Errorf("table content missing:\n%s", joined)
	}
}

func TestMarkdownTableIncompleteStream(t *testing.T) {
	md := "| Column A | Column B |"
	lines := renderMarkdown(md, 80, theme.Default())
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Column A") || !strings.Contains(joined, "Column B") {
		t.Errorf("incomplete table header lost:\n%s", joined)
	}
}

func TestMarkdownTableAroundFences(t *testing.T) {
	md := "```\ncat file.txt | grep foo\n```\n| Item | Count |\n|---|---:|\n| Apples | 10 |\n"
	lines := renderMarkdown(md, 80, theme.Default())
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "cat file.txt | grep foo") {
		t.Errorf("code line missing:\n%s", joined)
	}
	if !strings.Contains(joined, "Apples") || !strings.Contains(joined, "10") {
		t.Errorf("table content missing:\n%s", joined)
	}
}
