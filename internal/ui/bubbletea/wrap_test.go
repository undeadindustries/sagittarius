package bubbletea

import (
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

func TestSanitizeDisplayText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"normal text", "normal text"},
		{"line\r\nbreak", "line\nbreak"},
		{"line\rbreak", "linebreak"},
		{"tab\tone", "tab     one"},
		{"\tone", "        one"},
		{"12\t34", "12      34"},
		{"1234567\t8", "1234567 8"},
		{"12345678\t9", "12345678        9"},
		{"\x1b[31mred\x1b[0m text", "red text"},
		{"multi\n\tline", "multi\n        line"},
		{"\r\n\t", "\n        "},
	}
	for _, c := range cases {
		if got := sanitizeDisplayText(c.in); got != c.want {
			t.Errorf("sanitizeDisplayText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

var wrapVerbatimWidths = []int{40, 60, 80, 120}

func TestWrapVerbatimWidthInvariant(t *testing.T) {
	t.Parallel()
	inputs := []struct {
		name string
		text string
	}{
		{"no-space run", strings.Repeat("a", 200)},
		{"long URL", "https://example.com/" + strings.Repeat("pathsegment/", 20) + "file.json"},
		{"cjk run", strings.Repeat("中", 80)},
		{"tab-indented go", "\tfunc " + strings.Repeat("VeryLongIdentifier", 10) + "() {}"},
		{"all whitespace", "        "},
		{"empty", ""},
		{"mixed short and long", "ok " + strings.Repeat("x", 200) + " done"},
	}
	for _, in := range inputs {
		for _, w := range wrapVerbatimWidths {
			t.Run(in.name+"/"+strconv.Itoa(w), func(t *testing.T) {
				t.Parallel()
				rows := wrapVerbatim(in.text, w)
				for i, row := range rows {
					if got := lipgloss.Width(row); got > w {
						t.Errorf("row %d width %d exceeds %d: %q", i, got, w, row)
					}
				}
			})
		}
	}
}

func TestWrapVerbatimPreservesIndentAndCharacters(t *testing.T) {
	t.Parallel()

	const indent = "    "
	body := strings.Repeat("x", 80)
	rows := wrapVerbatim(indent+body, 40)
	if len(rows) < 2 {
		t.Fatalf("expected wrap, got %d rows: %q", len(rows), rows)
	}
	if !strings.HasPrefix(rows[0], indent) {
		t.Errorf("first row lost indent: %q", rows[0])
	}
	for i, row := range rows[1:] {
		if strings.HasPrefix(row, indent) {
			t.Errorf("continuation row %d has invented indent: %q", i+1, row)
		}
	}
	if got := strings.Join(rows, ""); got != indent+body {
		t.Errorf("character loss:\n got %q\nwant %q", got, indent+body)
	}
}

func TestWrapVerbatimExpandsTabsWithoutInventedIndent(t *testing.T) {
	t.Parallel()
	src := "\t" + strings.Repeat("abcdefghij", 12)
	rows := wrapVerbatim(src, 40)
	if len(rows) < 2 {
		t.Fatalf("expected wrap, got %d rows: %q", len(rows), rows)
	}
	if !strings.HasPrefix(rows[0], "        ") {
		t.Errorf("tab indent not expanded on first row: %q", rows[0])
	}
	for i, row := range rows[1:] {
		if row != "" && unicode.IsSpace([]rune(row)[0]) {
			t.Errorf("continuation row %d starts with invented space: %q", i+1, row)
		}
	}
}

func TestWrapVerbatimEmptyAndWhitespace(t *testing.T) {
	t.Parallel()
	empty := wrapVerbatim("", 40)
	if len(empty) != 1 || empty[0] != "" {
		t.Errorf("empty = %q, want one empty row", empty)
	}
	ws := wrapVerbatim("     ", 40)
	if len(ws) != 1 || ws[0] != "     " {
		t.Errorf("whitespace = %q, want the original spaces", ws)
	}
}
