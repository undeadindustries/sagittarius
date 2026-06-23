package diff

import (
	"strings"
	"testing"
)

func TestUnifiedDiffIdentical(t *testing.T) {
	t.Parallel()
	if got := UnifiedDiff("same\n", "same\n", "x.txt"); got != "" {
		t.Fatalf("identical inputs should diff to empty, got %q", got)
	}
}

func TestUnifiedDiffChange(t *testing.T) {
	t.Parallel()
	got := UnifiedDiff("alpha\nbeta\ngamma\n", "alpha\nBETA\ngamma\n", "x.txt")
	for _, want := range []string{"--- a/x.txt", "+++ b/x.txt", "@@", "-beta", "+BETA", " alpha", " gamma"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diff missing %q:\n%s", want, got)
		}
	}
}

func TestUnifiedDiffAddToEmpty(t *testing.T) {
	t.Parallel()
	got := UnifiedDiff("", "one\ntwo\n", "new.txt")
	if !strings.Contains(got, "+one") || !strings.Contains(got, "+two") {
		t.Fatalf("add-to-empty diff wrong:\n%s", got)
	}
}

func TestUnifiedDiffLargeInputSummarized(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("line\n", 2100)
	got := UnifiedDiff(big, big+"extra\n", "big.txt")
	if !strings.Contains(got, "diff too large") {
		t.Fatalf("expected large-input summary, got %d bytes", len(got))
	}
}
