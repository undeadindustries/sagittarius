package tools

import (
	"strings"
	"testing"
)

func TestParseWriteLease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     any
		want    []string
		wantErr string
	}{
		{name: "strings slice", raw: []string{"a.go"}, want: []string{"a.go"}},
		{name: "any slice", raw: []any{"a.go", "pkg/**"}, want: []string{"a.go", "pkg/**"}},
		{name: "trailing slash expands", raw: []any{"tests/"}, want: []string{"tests/**"}},
		{name: "dot prefix stripped", raw: []any{"./a.go"}, want: []string{"a.go"}},
		{name: "duplicates collapsed", raw: []any{"a.go", "./a.go"}, want: []string{"a.go"}},
		{name: "nil", raw: nil, wantErr: "missing"},
		{name: "empty list", raw: []any{}, wantErr: "empty"},
		{name: "wrong element type", raw: []any{1}, wantErr: "want string"},
		{name: "wrong container", raw: "a.go", wantErr: "want a list"},
		{name: "absolute", raw: []any{"/etc/passwd"}, wantErr: "absolute"},
		{name: "escape", raw: []any{"../outside.go"}, wantErr: "escapes the workspace"},
		{name: "nested escape", raw: []any{"pkg/../../outside.go"}, wantErr: "escapes the workspace"},
		{name: "blank entry", raw: []any{"  "}, wantErr: "empty pattern"},
		{name: "dot only", raw: []any{"."}, wantErr: "does not name anything"},
		{name: "malformed glob", raw: []any{"pkg/[a-"}, wantErr: "malformed"},
		{name: "too deep", raw: []any{strings.Repeat("a/", maxLeaseSegments+1) + "b"}, wantErr: "segments deep"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseWriteLease(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got.Patterns, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("patterns = %v, want %v", got.Patterns, tc.want)
			}
		})
	}
}

func TestParseWriteLeaseTooManyPatterns(t *testing.T) {
	t.Parallel()
	raw := make([]any, maxLeasePatterns+1)
	for i := range raw {
		raw[i] = "f" + strings.Repeat("x", i) + ".go"
	}
	if _, err := ParseWriteLease(raw); err == nil || !strings.Contains(err.Error(), "limit is") {
		t.Fatalf("err = %v, want pattern limit error", err)
	}
}

func TestWriteLeaseAllows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		lease   []string
		path    string
		allowed bool
	}{
		{name: "exact", lease: []string{"a.go"}, path: "a.go", allowed: true},
		{name: "exact mismatch", lease: []string{"a.go"}, path: "b.go"},
		{name: "single star same dir", lease: []string{"pkg/*.go"}, path: "pkg/a.go", allowed: true},
		{name: "single star does not cross dirs", lease: []string{"pkg/*.go"}, path: "pkg/sub/a.go"},
		{name: "double star deep", lease: []string{"pkg/**"}, path: "pkg/sub/deep/a.go", allowed: true},
		{name: "double star matches zero segments", lease: []string{"pkg/**"}, path: "pkg", allowed: true},
		{name: "leading double star", lease: []string{"**/test_*.py"}, path: "a/b/test_x.py", allowed: true},
		{name: "leading double star at root", lease: []string{"**/test_*.py"}, path: "test_x.py", allowed: true},
		{name: "sibling dir denied", lease: []string{"pkg/**"}, path: "other/a.go"},
		{name: "prefix is not a match", lease: []string{"pkg"}, path: "pkg/a.go"},
		{name: "dot slash target", lease: []string{"a.go"}, path: "./a.go", allowed: true},
		{name: "empty target", lease: []string{"**"}, path: ""},
		{name: "second pattern matches", lease: []string{"x.go", "y.go"}, path: "y.go", allowed: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, err := ParseWriteLease(tc.lease)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := l.Allows(tc.path); got != tc.allowed {
				t.Fatalf("Allows(%q) = %v, want %v (lease %v)", tc.path, got, tc.allowed, l.Patterns)
			}
		})
	}
}

func TestLeasesOverlap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    []string
		overlap bool
	}{
		{name: "disjoint files", a: []string{"a.go"}, b: []string{"b.go"}},
		{name: "identical files", a: []string{"a.go"}, b: []string{"a.go"}, overlap: true},
		{name: "disjoint dirs", a: []string{"pkg/a/**"}, b: []string{"pkg/b/**"}},
		{name: "nested dirs", a: []string{"pkg/**"}, b: []string{"pkg/a/x.go"}, overlap: true},
		{name: "glob covers literal", a: []string{"pkg/*.go"}, b: []string{"pkg/a.go"}, overlap: true},
		{name: "glob misses literal", a: []string{"pkg/*.go"}, b: []string{"pkg/a.py"}},
		{name: "two globs fail closed", a: []string{"pkg/*.go"}, b: []string{"pkg/a*"}, overlap: true},
		{name: "root recursive covers all", a: []string{"**"}, b: []string{"deep/nested/a.go"}, overlap: true},
		{name: "recursive covers its own root", a: []string{"src/**"}, b: []string{"src"}, overlap: true},
		{name: "different depth literals", a: []string{"a/b"}, b: []string{"a/b/c"}},
		{name: "one of several collides", a: []string{"x.go", "pkg/a.go"}, b: []string{"pkg/a.go"}, overlap: true},
		{name: "leading recursive both sides", a: []string{"**/x.go"}, b: []string{"pkg/**"}, overlap: true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			la, err := ParseWriteLease(tc.a)
			if err != nil {
				t.Fatalf("parse a: %v", err)
			}
			lb, err := ParseWriteLease(tc.b)
			if err != nil {
				t.Fatalf("parse b: %v", err)
			}
			who, got := LeasesOverlap(la, lb)
			if got != tc.overlap {
				t.Fatalf("LeasesOverlap(%v, %v) = %v (%q), want %v", la.Patterns, lb.Patterns, got, who, tc.overlap)
			}
			// Overlap is symmetric.
			if _, rev := LeasesOverlap(lb, la); rev != got {
				t.Fatalf("overlap not symmetric: %v vs %v", got, rev)
			}
			if got && who == "" {
				t.Fatal("overlap reported without naming the colliding patterns")
			}
		})
	}
}
