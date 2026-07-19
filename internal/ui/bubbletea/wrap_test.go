package bubbletea

import (
	"testing"
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
