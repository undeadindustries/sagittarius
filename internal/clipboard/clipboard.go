// Package clipboard copies text to the system clipboard, preferring a local
// clipboard mechanism (pbcopy/xclip/wl-copy/clip.exe via atotto/clipboard) and
// exposing an OSC 52 terminal escape sequence for remote sessions, where the
// local mechanism (if any) belongs to the wrong machine.
package clipboard

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/atotto/clipboard"
)

// ErrUnavailable indicates no local clipboard mechanism is available; callers
// should fall back to the OSC 52 escape sequence (see OSC52Sequence).
var ErrUnavailable = errors.New("no local clipboard mechanism available")

// screenChunkSize is the largest payload GNU screen forwards inside a single
// DCS string; longer payloads are split across consecutive DCS strings.
const screenChunkSize = 76

// Available reports whether a local clipboard mechanism is present.
func Available() bool {
	return !clipboard.Unsupported
}

// Copy writes text to the local system clipboard. It returns ErrUnavailable
// when no local mechanism exists, so the caller can fall back to OSC 52.
func Copy(text string) error {
	if clipboard.Unsupported {
		return ErrUnavailable
	}
	if err := clipboard.WriteAll(text); err != nil {
		return fmt.Errorf("write clipboard: %w", err)
	}
	return nil
}

// IsRemoteSession reports whether the process runs inside an SSH session. In
// that case a local clipboard binary (xclip, wl-copy) writes to the server's
// display, not the user's, so OSC 52 is the only path to the user's clipboard.
func IsRemoteSession(getenv func(string) string) bool {
	return getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" || getenv("SSH_CLIENT") != ""
}

// OSC52Sequence returns the OSC 52 terminal escape sequence that instructs a
// supporting terminal emulator to set its clipboard to text.
func OSC52Sequence(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

// OSC52SequenceForEnv returns the OSC 52 sequence wrapped for the terminal
// multiplexer the environment reports. Inside tmux it emits the plain sequence
// (honored when tmux's set-clipboard is on/external, the default) followed by a
// DCS passthrough copy (honored when allow-passthrough is on), so either tmux
// configuration reaches the outer terminal without user changes; a terminal
// that receives both sets the same text twice. Inside GNU screen the payload is
// split into chunked DCS strings, which screen forwards unchanged.
func OSC52SequenceForEnv(text string, getenv func(string) string) string {
	plain := OSC52Sequence(text)
	switch {
	case getenv("TMUX") != "":
		// tmux passthrough requires every ESC inside the DCS body to be doubled.
		return plain + "\x1bPtmux;" + strings.ReplaceAll(plain, "\x1b", "\x1b\x1b") + "\x1b\\"
	case getenv("STY") != "" || strings.HasPrefix(getenv("TERM"), "screen"):
		return screenWrap(base64.StdEncoding.EncodeToString([]byte(text)))
	default:
		return plain
	}
}

// screenWrap builds <DCS><OSC52 start><chunk><ST><DCS><chunk>...<BEL><ST>.
func screenWrap(b64 string) string {
	var b strings.Builder
	b.WriteString("\x1bP\x1b]52;c;")
	for i := 0; i < len(b64); i += screenChunkSize {
		if i > 0 {
			b.WriteString("\x1b\\\x1bP")
		}
		b.WriteString(b64[i:min(i+screenChunkSize, len(b64))])
	}
	b.WriteString("\a\x1b\\")
	return b.String()
}

// WriteOSC52 writes the environment-appropriate OSC 52 sequence for text to w.
// Callers inside a full-screen renderer must write to the terminal directly:
// Bubble Tea drops tea.Printf output while the alternate screen is active.
func WriteOSC52(w io.Writer, text string, getenv func(string) string) error {
	if _, err := io.WriteString(w, OSC52SequenceForEnv(text, getenv)); err != nil {
		return fmt.Errorf("write osc52: %w", err)
	}
	return nil
}
