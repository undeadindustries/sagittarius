package bubbletea

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

// TestStyleInlineFlanking pins the visible text styleInline produces. Every
// expectation was verified against a CommonMark reference implementation
// (markdown-it in commonmark mode) except the three dunder cases, which are a
// deliberate departure documented in mdinline.go: the spec bolds __init__
// because it is whitespace-flanked, which mangles source text.
func TestStyleInlineFlanking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		// Identifiers must survive: this is the reported defect.
		{"snake case method", "rewards.apply_xp_and_level_up has levels_gained += 1", "rewards.apply_xp_and_level_up has levels_gained += 1"},
		{"snake case test name", "test_multi_level_up_with_carry_over: 110 XP", "test_multi_level_up_with_carry_over: 110 XP"},
		{"trailing numeric segment", "test_equip_owned_item_returns_400: my next(...) ran", "test_equip_owned_item_returns_400: my next(...) ran"},
		{"snake case prose", "snake_case_var and MY_CONST", "snake_case_var and MY_CONST"},
		{"file path", "/home/me/cache/browser_screenshot_ecc1c3feab.png", "/home/me/cache/browser_screenshot_ecc1c3feab.png"},
		{"dotted filenames", "see file_a.py and file_b.py", "see file_a.py and file_b.py"},
		{"intraword double underscore", "foo__bar__baz", "foo__bar__baz"},

		// Dunders: kept literal, unlike strict CommonMark.
		{"dunder init", "def __init__(self) -> None", "def __init__(self) -> None"},
		{"dunder main guard", `if __name__ == "__main__":`, `if __name__ == "__main__":`},
		{"dunder file", "print(__file__)", "print(__file__)"},

		// Asterisk delimiters with no valid closer stay literal.
		{"python star args", "pass *args and *kwargs through", "pass *args and *kwargs through"},
		{"c pointer syntax", "uint8_t* base = (uint8_t*)0x20000000;", "uint8_t* base = (uint8_t*)0x20000000;"},
		{"multiplication", "2 * 3 * 4 = 24", "2 * 3 * 4 = 24"},

		// Real emphasis still renders, including intraword asterisks.
		{"single underscore italic", "say _hi_ there", "say hi there"},
		{"multiword underscore bold", "very __bold move__ today", "very bold move today"},
		{"bracket flanked italic", "(_paren_) and [_bracket_]", "(paren) and [bracket]"},
		{"intraword asterisks", "a*b*c and a**bold**c", "abc and aboldc"},
		{"asterisk bold and italic", "**real bold** and *real italic*", "real bold and real italic"},
		{"triple asterisk", "***both*** emphasized", "both emphasized"},
		{"nested emphasis", "**bold with _inner_ text**", "bold with inner text"},

		// Code spans are opaque and consume only their backticks.
		{"code span underscores", "`code_span_here` stays", "code_span_here stays"},
		{"code span with emphasis chars", "call `a*b` then *go*", "call a*b then go"},
		{"unterminated code span", "an ` orphan tick", "an ` orphan tick"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripANSI(styleInline(tc.in, theme.Greyscale()))
			if got != tc.want {
				t.Errorf("styleInline(%q):\n got: %q\nwant: %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStyleInlineAppliesStyling guards against the scanner degrading into a
// marker stripper: accepted emphasis must still emit ANSI, and text we refuse
// to interpret must stay completely unstyled.
func TestStyleInlineAppliesStyling(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	th := theme.Default()

	styled := []string{"say _hi_ there", "**bold**", "*italic*", "***both***", "very __bold move__ today"}
	for _, in := range styled {
		out := styleInline(in, th)
		if out == stripANSI(out) {
			t.Errorf("expected styling for %q, got plain %q", in, out)
		}
	}

	literal := []string{
		"rewards.apply_xp_and_level_up",
		"def __init__(self)",
		"pass *args and *kwargs",
		"uint8_t* base = (uint8_t*)0;",
	}
	for _, in := range literal {
		out := styleInline(in, th)
		if out != in {
			t.Errorf("expected %q untouched, got %q", in, out)
		}
	}
}

// TestMarkdownPreservesIdentifiersEndToEnd runs the reported output through the
// full renderer, including word wrapping, since styleInline sees one already
// wrapped line at a time.
func TestMarkdownPreservesIdentifiersEndToEnd(t *testing.T) {
	t.Parallel()

	md := "The bug is confirmed: rewards.apply_xp_and_level_up has levels_gained += 1 on both " +
		"line 84 and line 93, doubling the count.\n\n" +
		"1. test_multi_level_up_with_carry_over: 110 XP from level 1\n" +
		"2. test_equip_owned_item_returns_400: my next(...) ran on the original list\n"

	got := stripANSI(strings.Join(renderMarkdown(md, 100, theme.Greyscale()), "\n"))
	for _, ident := range []string{
		"rewards.apply_xp_and_level_up",
		"levels_gained",
		"test_multi_level_up_with_carry_over",
		"test_equip_owned_item_returns_400",
	} {
		if !strings.Contains(got, ident) {
			t.Errorf("identifier %q was mangled:\n%s", ident, got)
		}
	}
}

// TestStyleInlineCodeSpanDoesNotLeakIntoFlanking is the reason code spans are
// scanned in the same pass as emphasis. Rendering them first left ANSI escapes
// in the text, and the trailing "m" of a reset sequence read as a word
// character, so an adjacent delimiter looked intraword and was rejected.
func TestStyleInlineCodeSpanDoesNotLeakIntoFlanking(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	out := styleInline("`code`*italic*", theme.Default())
	if got := stripANSI(out); got != "codeitalic" {
		t.Fatalf("markers not consumed: %q", got)
	}
	if !strings.Contains(out, "\x1b[3m") {
		t.Errorf("emphasis after a code span lost its italic styling: %q", out)
	}
}
