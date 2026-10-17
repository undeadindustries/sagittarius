package overlay

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

func numberedRows(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("row %02d", i)
	}
	return rows
}

func TestListWindow(t *testing.T) {
	tests := []struct {
		name                 string
		total, cursor, budge int
		wantStart, wantEnd   int
	}{
		{"fits", 5, 4, 10, 0, 5},
		{"exact fit", 5, 0, 5, 0, 5},
		{"cursor at start", 20, 0, 7, 0, 5},
		{"cursor in middle", 20, 10, 7, 8, 13},
		{"cursor at end", 20, 19, 7, 15, 20},
		{"budget of one still shows cursor", 20, 12, 1, 12, 13},
		{"negative budget still shows cursor", 20, 3, -4, 3, 4},
		{"cursor out of range is clamped", 20, 99, 7, 15, 20},
		{"empty list", 0, 0, 5, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, e := listWindow(tt.total, tt.cursor, tt.budge)
			if s != tt.wantStart || e != tt.wantEnd {
				t.Fatalf("listWindow(%d, %d, %d) = [%d,%d), want [%d,%d)",
					tt.total, tt.cursor, tt.budge, s, e, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestFitFrameFitsHeight(t *testing.T) {
	th := theme.Greyscale()
	tests := []struct {
		name       string
		box        Box
		list       List
		wantLines  int // exact height when > 0
		mustHave   []string
		mustNotSee []string
	}{
		{
			name:      "short list renders whole",
			box:       Box{Width: 60, Height: 30},
			list:      List{Head: "Title\n", Rows: numberedRows(3), Foot: "\nhints"},
			mustHave:  []string{"Title", "row 00", "row 02", "hints"},
			wantLines: 2 + 2 + 3 + 2,
		},
		{
			name:       "long list windows around cursor",
			box:        Box{Width: 60, Height: 12},
			list:       List{Head: "Title\n", Rows: numberedRows(40), Foot: "\nhints", Cursor: 20},
			mustHave:   []string{"Title", "row 20", "more above", "more below", "hints"},
			mustNotSee: []string{"row 00", "row 39"},
		},
		{
			name: "wrapped head is measured",
			box:  Box{Width: 30, Height: 12},
			list: List{
				Head: "Title\n" + strings.Repeat("word ", 20) + "\n",
				Rows: numberedRows(40), Foot: "hints", Cursor: 39,
			},
			mustHave: []string{"Title", "row 39", "hints"},
		},
		{
			name:     "unknown height renders everything",
			box:      Box{Width: 60},
			list:     List{Head: "Title", Rows: numberedRows(40)},
			mustHave: []string{"row 00", "row 39"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := FitFrame(th, tt.box, tt.list)
			h := lipgloss.Height(out)
			if tt.box.Height > 0 && h > tt.box.Height {
				t.Fatalf("height %d exceeds terminal %d:\n%s", h, tt.box.Height, out)
			}
			if tt.wantLines > 0 && h != tt.wantLines {
				t.Errorf("height = %d, want %d:\n%s", h, tt.wantLines, out)
			}
			for _, s := range tt.mustHave {
				if !strings.Contains(out, s) {
					t.Errorf("output missing %q:\n%s", s, out)
				}
			}
			for _, s := range tt.mustNotSee {
				if strings.Contains(out, s) {
					t.Errorf("output should not contain %q:\n%s", s, out)
				}
			}
		})
	}
}

func TestFitListTruncatesWideRows(t *testing.T) {
	box := Box{Width: 30, Height: 20}
	out := FitFrame(theme.Greyscale(), box, List{Rows: []string{strings.Repeat("x", 200)}})
	if h := lipgloss.Height(out); h != 3 {
		t.Fatalf("a wide row wrapped: height %d, want 3:\n%s", h, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("truncated row has no ellipsis:\n%s", out)
	}
}

func TestFitHeight(t *testing.T) {
	th := theme.Greyscale()
	tall := strings.Join(numberedRows(10), "\n")
	tests := []struct {
		name   string
		view   string
		height int
		want   []string
	}{
		{"fits unchanged", tall, 10, numberedRows(10)},
		{"unknown height unchanged", tall, 0, numberedRows(10)},
		{"clipped keeps top", tall, 4, []string{"row 00", "row 01", "row 02", "… enlarge the terminal to see more"}},
		{"height one keeps first line", tall, 1, []string{"row 00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Split(ansi.Strip(FitHeight(th, tt.view, tt.height)), "\n")
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("FitHeight = %q, want %q", got, tt.want)
			}
		})
	}
}
