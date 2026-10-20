package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
)

func TestSplitPreexistingFindings(t *testing.T) {
	t.Parallel()

	// utils.py changed only on line 14; everything else is untouched.
	changed := func(path string) (map[int]bool, bool) {
		if matchSessionFile(path, []string{"pkg/utils.py"}) == "" {
			return nil, false
		}
		return map[int]bool{14: true}, true
	}
	ruffFull := "E722 Do not use bare `except`\n --> pkg/utils.py:8:5\n  |\n8 |     except:\n  |     ^^^^^^\n\nFound 1 error."
	concise := "pkg/utils.py:8:5: E722 Do not use bare `except`\npkg/utils.py:14:1: F821 Undefined name `x`\nFound 2 errors."

	tests := []struct {
		name        string
		finding     diagnostics.Finding
		wantKept    []string
		wantOldMsgs []string
	}{
		{
			name:        "full format, only old lines",
			finding:     diagnostics.Finding{Tool: "ruff", Severity: diagnostics.SeverityWarning, Message: ruffFull},
			wantOldMsgs: []string{ruffFull},
		},
		{
			name:        "line-oriented mix is split",
			finding:     diagnostics.Finding{Tool: "ruff", Severity: diagnostics.SeverityWarning, Message: concise},
			wantKept:    []string{"pkg/utils.py:14:1: F821 Undefined name `x`"},
			wantOldMsgs: []string{"pkg/utils.py:8:5: E722 Do not use bare `except`"},
		},
		{
			name: "multi-line mix is kept whole",
			finding: diagnostics.Finding{Tool: "ruff", Severity: diagnostics.SeverityWarning,
				Message: "E722 bare except\n --> pkg/utils.py:8:5\nF821 undefined\n --> pkg/utils.py:14:1"},
			wantKept: []string{"E722 bare except\n --> pkg/utils.py:8:5\nF821 undefined\n --> pkg/utils.py:14:1"},
		},
		{
			name:     "errors are never filtered",
			finding:  diagnostics.Finding{Tool: "build", Severity: diagnostics.SeverityError, Message: "pkg/utils.py:8:5: broken"},
			wantKept: []string{"pkg/utils.py:8:5: broken"},
		},
		{
			name:     "absolute LSP path on a changed line is kept",
			finding:  diagnostics.Finding{Tool: "pyright", Severity: diagnostics.SeverityWarning, Message: "/work/pkg/utils.py:14 unused variable"},
			wantKept: []string{"/work/pkg/utils.py:14 unused variable"},
		},
		{
			name:     "file the session didn't change is kept",
			finding:  diagnostics.Finding{Tool: "ruff", Severity: diagnostics.SeverityWarning, Message: "other.py:3:1: F401 unused"},
			wantKept: []string{"other.py:3:1: F401 unused"},
		},
		{
			name: "no parseable location is kept",
			finding: diagnostics.Finding{Tool: "eslint", Severity: diagnostics.SeverityWarning,
				Message: "/work/app.js\n  8:5  error  no-unused-vars"},
			wantKept: []string{"/work/app.js\n  8:5  error  no-unused-vars"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			kept, old := splitPreexistingFindings([]diagnostics.Finding{tt.finding}, changed)
			assertMessages(t, "kept", kept, tt.wantKept)
			assertMessages(t, "preexisting", old, tt.wantOldMsgs)
		})
	}
}

func TestSplitPreexistingFindingsWithoutSnapshot(t *testing.T) {
	t.Parallel()
	in := []diagnostics.Finding{{Tool: "ruff", Severity: diagnostics.SeverityWarning, Message: "a.py:1:1: E1"}}
	kept, old := splitPreexistingFindings(in, nil)
	if len(kept) != 1 || len(old) != 0 {
		t.Fatalf("kept=%v old=%v, want everything kept when no snapshot exists", kept, old)
	}
}

func assertMessages(t *testing.T, label string, got []diagnostics.Finding, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %d findings %+v, want %d", label, len(got), got, len(want))
	}
	for i := range want {
		if got[i].Message != want[i] {
			t.Fatalf("%s[%d] = %q, want %q", label, i, got[i].Message, want[i])
		}
	}
}

func TestMatchSessionFile(t *testing.T) {
	t.Parallel()
	files := []string{"pkg/utils.py", "main.go"}
	tests := []struct{ reported, want string }{
		{"pkg/utils.py", "pkg/utils.py"},
		{"./pkg/utils.py", "pkg/utils.py"},
		{"/home/u/proj/pkg/utils.py", "pkg/utils.py"},
		{"utils.py", "pkg/utils.py"},
		{"cmd/main.go", ""},
		{"other.py", ""},
	}
	for _, tt := range tests {
		if got := matchSessionFile(tt.reported, files); got != tt.want {
			t.Errorf("matchSessionFile(%q) = %q, want %q", tt.reported, got, tt.want)
		}
	}
}
