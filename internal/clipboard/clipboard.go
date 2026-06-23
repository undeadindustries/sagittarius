// Package clipboard copies text to the system clipboard, preferring a local
// clipboard mechanism (pbcopy/xclip/wl-copy/clip.exe via atotto/clipboard) and
// exposing an OSC 52 terminal escape sequence as a fallback for remote sessions.
package clipboard

import (
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/atotto/clipboard"
)

// ErrUnavailable indicates no local clipboard mechanism is available; callers
// should fall back to the OSC 52 escape sequence (see OSC52Sequence).
var ErrUnavailable = errors.New("no local clipboard mechanism available")

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

// OSC52Sequence returns the OSC 52 terminal escape sequence that instructs a
// supporting terminal emulator to set its clipboard to text. It is the fallback
// for remote sessions where no local clipboard binary is reachable.
func OSC52Sequence(text string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + encoded + "\a"
}
