package bubbletea

import (
	"strings"
	"testing"
)

func TestPasteStoreCapture(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "small paste not collapsed",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "exactly 5 lines not collapsed",
			in:   "1\n2\n3\n4\n5",
			want: "1\n2\n3\n4\n5",
		},
		{
			name: "6 lines collapsed",
			in:   "1\n2\n3\n4\n5\n6",
			want: "[Pasted Text: 6 lines]",
		},
		{
			name: "500 chars not collapsed",
			in:   strings.Repeat("a", 500),
			want: strings.Repeat("a", 500),
		},
		{
			name: "501 chars collapsed",
			in:   strings.Repeat("a", 501),
			want: "[Pasted Text: 501 chars]",
		},
		{
			name: "normalize line endings",
			in:   "1\r\n2\r\n3\r\n4\r\n5\r\n6",
			want: "[Pasted Text: 6 lines]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newPasteStore()
			got := store.capture(tc.in)
			if got != tc.want {
				t.Fatalf("capture() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPasteStoreCollisions(t *testing.T) {
	store := newPasteStore()
	in := "1\n2\n3\n4\n5\n6"

	got1 := store.capture(in)
	if got1 != "[Pasted Text: 6 lines]" {
		t.Fatalf("first capture = %q", got1)
	}

	got2 := store.capture(in)
	if got2 != "[Pasted Text: 6 lines #2]" {
		t.Fatalf("second capture = %q", got2)
	}

	got3 := store.capture(in)
	if got3 != "[Pasted Text: 6 lines #3]" {
		t.Fatalf("third capture = %q", got3)
	}
}

func TestPasteStoreExpand(t *testing.T) {
	store := newPasteStore()
	ph := store.capture("1\n2\n3\n4\n5\n6")

	got := store.expand("Prefix " + ph + " Suffix")
	want := "Prefix 1\n2\n3\n4\n5\n6 Suffix"

	if got != want {
		t.Fatalf("expand() = %q, want %q", got, want)
	}
}

func TestPasteStorePrune(t *testing.T) {
	store := newPasteStore()
	ph1 := store.capture("1\n2\n3\n4\n5\n6")
	ph2 := store.capture("a\nb\nc\nd\ne\nf")

	// Both present, shouldn't change
	changed := store.prune(ph1 + " " + ph2)
	if changed {
		t.Fatal("prune() reported changed when both present")
	}

	// Remove ph1
	changed = store.prune(ph2)
	if !changed {
		t.Fatal("prune() didn't report changed when dropping one")
	}
	if len(store.content) != 1 || store.content[ph2] == "" {
		t.Fatal("store.content has wrong state")
	}

	// Remove ph2
	store.prune("empty")
	if len(store.content) != 0 {
		t.Fatal("store.content should be empty")
	}
}

func TestPasteStorePruneExpanded(t *testing.T) {
	store := newPasteStore()
	ph := store.capture("1\n2\n3\n4\n5\n6")

	// Set expanded
	store.expanded = &expandedPaste{id: ph}

	// Full text present -> no prune
	changed := store.prune("Prefix 1\n2\n3\n4\n5\n6 Suffix")
	if changed || store.expanded == nil || len(store.content) == 0 {
		t.Fatal("pruned valid expanded paste")
	}

	// Text edited -> pruned
	changed = store.prune("Prefix 1\n2\nEDITED\n4\n5\n6 Suffix")
	if !changed || store.expanded != nil || len(store.content) != 0 {
		t.Fatal("failed to prune edited expanded paste")
	}
}

func TestPlaceholderAt(t *testing.T) {
	s := "Prefix [Pasted Text: 6 lines] Suffix"

	// Match inside
	id, start, end, ok := placeholderAt(s, 10)
	if !ok || id != "[Pasted Text: 6 lines]" || start != 7 || end != 29 {
		t.Errorf("placeholderAt(10) = %q, %d, %d, %v", id, start, end, ok)
	}

	// Match left edge
	id, _, _, ok = placeholderAt(s, 7)
	if !ok || id != "[Pasted Text: 6 lines]" {
		t.Errorf("placeholderAt(7) edge failed")
	}

	// Match right edge
	id, _, _, ok = placeholderAt(s, 29)
	if !ok || id != "[Pasted Text: 6 lines]" {
		t.Errorf("placeholderAt(29) edge failed")
	}

	// No match
	_, _, _, ok = placeholderAt(s, 6)
	if ok {
		t.Errorf("placeholderAt(6) should fail")
	}
}
