package bubbletea

import (
	"errors"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// TestCopyToClipboardIsAsync verifies the copy command defers the (blocking)
// clipboard write to a tea.Cmd that reports back via clipboardResultMsg, rather
// than performing it inline on the UI goroutine.
func TestCopyToClipboardIsAsync(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	cmd := m.copyToClipboard("payload")
	if cmd == nil {
		t.Fatal("copyToClipboard returned nil cmd")
	}
	// No scrollback should be written until the async result is handled.
	if len(m.blocks) != 0 {
		t.Fatalf("expected no blocks before result, got %d", len(m.blocks))
	}
	msg, ok := cmd().(clipboardResultMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want clipboardResultMsg", cmd())
	}
	if msg.text != "payload" {
		t.Fatalf("result text = %q, want %q", msg.text, "payload")
	}
}

func TestHandleClipboardResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		wantBlock scrollRole
		wantText  string
		wantCmd   bool
	}{
		{"local", nil, roleInfo, "Copied last response to the clipboard.", false},
		{"osc52", clipboard.ErrUnavailable, roleInfo, "OSC 52", true},
		{"failure", errors.New("boom"), roleError, "Clipboard copy failed: boom", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModel()
			cmd := m.handleClipboardResult(clipboardResultMsg{text: "x", err: tc.err})
			if (cmd != nil) != tc.wantCmd {
				t.Fatalf("cmd present = %v, want %v", cmd != nil, tc.wantCmd)
			}
			if len(m.blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(m.blocks))
			}
			if m.blocks[0].role != tc.wantBlock {
				t.Fatalf("block role = %v, want %v", m.blocks[0].role, tc.wantBlock)
			}
			if !strings.Contains(m.blocks[0].text, tc.wantText) {
				t.Fatalf("block text %q missing %q", m.blocks[0].text, tc.wantText)
			}
		})
	}
}

// TestStreamCopyToClipboardDispatches verifies the stream event yields a command
// (the async copy) and writes nothing to scrollback synchronously.
func TestStreamCopyToClipboardDispatches(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	_, cmd := m.handleStream(ui.StreamEvent{Type: ui.StreamCopyToClipboard, Text: "hello"})
	if cmd == nil {
		t.Fatal("expected a command from StreamCopyToClipboard")
	}
	if len(m.blocks) != 0 {
		t.Fatalf("expected no synchronous blocks, got %d", len(m.blocks))
	}
}
