package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	m, err := NewManager(root, "baseline-test", Options{MaxFileBytes: 64})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	write := func(name, content string) {
		t.Helper()
		abs := filepath.Join(root, name)
		m.CaptureWrite(abs)
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		m.CommitWrite(abs, "write_file")
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("a.txt", "first\n")
	write("a.txt", "second\n")
	write("new.txt", "created\n")
	big := make([]byte, 128)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	write("big.txt", string(big)+"y")

	tests := []struct {
		rel  string
		want Baseline
	}{
		{"a.txt", Baseline{Content: "original\n", Tracked: true, Captured: true}},
		{"new.txt", Baseline{Content: "", Tracked: true, Captured: true}},
		{"big.txt", Baseline{Tracked: true}},
		{"untouched.txt", Baseline{}},
	}
	for _, tt := range tests {
		if got := m.SessionBaseline(tt.rel); got != tt.want {
			t.Errorf("SessionBaseline(%q) = %+v, want %+v", tt.rel, got, tt.want)
		}
	}
	var nilManager *Manager
	if got := nilManager.SessionBaseline("a.txt"); got != (Baseline{}) {
		t.Errorf("nil manager = %+v, want zero", got)
	}
}
