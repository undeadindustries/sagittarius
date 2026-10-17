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
	m.localCopy = func(string) error { return nil }
	m.getenv = func(string) string { return "" }
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
		msg       clipboardResultMsg
		wantBlock scrollRole
		wantText  string
	}{
		{"local", clipboardResultMsg{}, roleInfo, "Copied to the clipboard."},
		{"osc52 fallback", clipboardResultMsg{err: clipboard.ErrUnavailable, osc52: true}, roleInfo, "OSC 52"},
		{"osc52 remote", clipboardResultMsg{osc52: true}, roleInfo, "OSC 52"},
		{"failure", clipboardResultMsg{err: errors.New("boom")}, roleError, "Clipboard copy failed: boom"},
		{"both fail", clipboardResultMsg{err: errors.New("boom"), osc52: true, oscErr: errors.New("tty gone")}, roleError, "tty gone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModel()
			tc.msg.text = "x"
			if cmd := m.handleClipboardResult(tc.msg); cmd != nil {
				t.Fatal("handleClipboardResult should not return a command; OSC 52 is written directly")
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

// TestCopySinkWritesOSC52 pins when the OSC 52 sequence reaches the terminal:
// always over SSH (a local copy there lands on the server), and locally only
// as a fallback when the system clipboard is unavailable.
func TestCopySinkWritesOSC52(t *testing.T) {
	t.Parallel()
	ok := func(string) error { return nil }
	fail := func(string) error { return clipboard.ErrUnavailable }
	tests := []struct {
		name      string
		local     func(string) error
		env       map[string]string
		wantOSC   bool
		wantTmux  bool
		wantLocal error
	}{
		{"local ok", ok, nil, false, false, nil},
		{"local fails", fail, nil, true, false, clipboard.ErrUnavailable},
		{"ssh", ok, map[string]string{"SSH_CONNECTION": "1 2 3 4"}, true, false, nil},
		{"ssh in tmux", fail, map[string]string{"SSH_TTY": "/dev/pts/1", "TMUX": "/tmp/t,1,0"}, true, true, clipboard.ErrUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf strings.Builder
			sink := copySink{local: tc.local, out: &buf, getenv: func(k string) string { return tc.env[k] }}
			res := sink.copy("hello", false)
			if res.osc52 != tc.wantOSC {
				t.Fatalf("osc52 = %v, want %v", res.osc52, tc.wantOSC)
			}
			if !errors.Is(res.err, tc.wantLocal) && res.err != tc.wantLocal {
				t.Fatalf("err = %v, want %v", res.err, tc.wantLocal)
			}
			wrote := strings.Contains(buf.String(), clipboard.OSC52Sequence("hello"))
			if wrote != tc.wantOSC {
				t.Fatalf("wrote OSC 52 = %v, want %v (out %q)", wrote, tc.wantOSC, buf.String())
			}
			if got := strings.Contains(buf.String(), "\x1bPtmux;"); got != tc.wantTmux {
				t.Fatalf("tmux passthrough = %v, want %v", got, tc.wantTmux)
			}
		})
	}
}

func TestClipboardResultWordingFollowsScreenMode(t *testing.T) {
	t.Parallel()
	remote := func(key string) string {
		if key == "SSH_CONNECTION" {
			return "1.2.3.4 1 5.6.7.8 22"
		}
		return ""
	}
	inline := newTestModel()
	inline.inline = true
	inline.getenv = remote
	inline.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if !strings.Contains(inline.blocks[0].text, "select the text above") {
		t.Fatalf("inline remote copy = %q", inline.blocks[0].text)
	}
	full := newTestModel()
	full.getenv = remote
	full.handleClipboardResult(clipboardResultMsg{text: "x", osc52: true})
	if !strings.Contains(full.blocks[0].text, "Terminal.app") || !strings.Contains(full.blocks[0].text, "/screen inline") {
		t.Fatalf("fullscreen remote copy = %q", full.blocks[0].text)
	}
	if !full.copyTipPending {
		t.Fatal("first remote OSC 52 copy should arm the SSH dialog")
	}
}

// TestSilentClipboardResultUsesStatusRow verifies a selection copy reports in
// the status row, never as a scrollback block that would shift the view.
func TestSilentClipboardResultUsesStatusRow(t *testing.T) {
	t.Parallel()
	m := newTestModel()
	m.handleClipboardResult(clipboardResultMsg{text: "héllo", silent: true})
	if len(m.blocks) != 0 {
		t.Fatalf("silent copy added %d blocks", len(m.blocks))
	}
	if m.copyHint != "Copied selection (5 chars)" {
		t.Fatalf("copyHint = %q", m.copyHint)
	}
	m.handleClipboardResult(clipboardResultMsg{text: "x", silent: true, err: errors.New("boom")})
	if !strings.Contains(m.copyHint, "boom") {
		t.Fatalf("copyHint = %q, want the error", m.copyHint)
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
