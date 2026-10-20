package diff

import (
	"reflect"
	"sort"
	"testing"
)

func TestChangedLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before string
		after  string
		want   []int
	}{
		{"identical", "a\nb\n", "a\nb\n", nil},
		{"edit one line", "a\nb\nc\n", "a\nB\nc\n", []int{1, 2}},
		{"insert", "a\nc\n", "a\nb\nc\n", []int{2}},
		{"append", "a\n", "a\nb\n", []int{2}},
		{"delete marks both neighbors", "a\nb\nc\n", "a\nc\n", []int{1, 2}},
		{"delete first line", "a\nb\n", "b\n", []int{1}},
		{"delete last line", "a\nb\n", "a\n", []int{1}},
		{"new file", "", "x\ny\n", []int{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ChangedLines(tt.before, tt.after)
			if !ok {
				t.Fatal("ok = false")
			}
			var lines []int
			for n := range got {
				lines = append(lines, n)
			}
			sort.Ints(lines)
			if !reflect.DeepEqual(lines, tt.want) {
				t.Fatalf("ChangedLines = %v, want %v", lines, tt.want)
			}
		})
	}
}
