package bubbletea

import (
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/undeadindustries/sagittarius/internal/clipboard"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// terminalProbeTimeout bounds the XTVERSION query. Terminals that answer DA1
// return well inside this. It only fires in full when a terminal answers neither.
const terminalProbeTimeout = time.Second

const (
	xtversionQuery = "\x1b[>0q"
	da1Query       = "\x1b[c"
)

// resolveScreenMode picks full screen or inline for one session.
// setting is fullscreen, inline, or auto. Anything else, including empty, is
// full screen. probe runs only for auto over SSH outside a multiplexer. A nil
// probe, or a probe that does not identify an OSC 52 terminal, yields inline.
func resolveScreenMode(setting string, remote, mux bool, probe func() (string, bool)) bool {
	switch setting {
	case config.UIScreenInline:
		return true
	case config.UIScreenAuto:
		if !remote {
			return false
		}
		if mux || probe == nil {
			return true
		}
		name, ok := probe()
		return !ok || !osc52ByDefault(name)
	default:
		return false
	}
}

// resolveForRun applies ui.screen to this process. Headless and screen-reader
// sessions stay on the existing path. The probe runs only when auto still
// needs a terminal identity, so the fullscreen default adds no startup wait.
func resolveForRun(opts ui.Options) bool {
	if opts.Headless || opts.ScreenReader {
		return false
	}
	setting, err := config.ParseUIScreen(opts.ScreenMode)
	if err != nil {
		setting = config.UIScreenFullscreen
	}
	remote := clipboard.IsRemoteSession(os.Getenv)
	mux := os.Getenv("TMUX") != "" || os.Getenv("STY") != ""
	return resolveScreenMode(setting, remote, mux, func() (string, bool) {
		return probeTerminal(terminalProbeTimeout)
	})
}

// osc52ByDefault reports whether an XTVERSION payload is a terminal that
// allows OSC 52 without a preference change. iTerm2 is excluded: clipboard
// access there is off until the user enables it.
func osc52ByDefault(version string) bool {
	s := strings.ToLower(version)
	switch {
	case strings.Contains(s, "kitty"),
		strings.Contains(s, "wezterm"),
		strings.Contains(s, "ghostty"),
		strings.Contains(s, "foot"):
		return true
	default:
		return false
	}
}

// parseXTVersion returns the payload of a DCS >| ... ST (or BEL) reply.
func parseXTVersion(raw string) (string, bool) {
	const marker = ">|"
	i := strings.Index(raw, marker)
	if i < 0 || !dcsBefore(raw[:i]) {
		return "", false
	}
	rest := raw[i+len(marker):]
	end := strings.Index(rest, "\x1b\\")
	if end < 0 {
		end = strings.IndexByte(rest, 0x07)
	}
	if end < 0 {
		return "", false
	}
	name := strings.TrimSpace(rest[:end])
	if name == "" {
		return "", false
	}
	return name, true
}

// dcsBefore reports whether marker sits in a DCS that started with ESC P.
func dcsBefore(prefix string) bool {
	esc := strings.LastIndex(prefix, "\x1b")
	if esc < 0 || esc+1 >= len(prefix) {
		return false
	}
	return prefix[esc+1] == 'P'
}

// da1Seen reports whether buf holds a primary device-attributes reply (CSI ... c).
func da1Seen(buf []byte) bool {
	s := string(buf)
	for len(s) > 0 {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return false
		}
		s = s[i+2:]
		final := -1
		for j := 0; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				final = j
				break
			}
		}
		if final < 0 {
			return false
		}
		if s[final] == 'c' {
			return true
		}
		s = s[final+1:]
	}
	return false
}

// probeTerminal asks the controlling terminal for XTVERSION. Failure, including
// no tty or a terminal that never answers, returns "", false so the caller
// picks inline mode.
func probeTerminal(timeout time.Duration) (string, bool) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", false
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Debug("terminal probe: close tty", "error", err)
		}
	}()
	return probeTTY(f, timeout)
}

// probeTTY writes XTVERSION and DA1 to f and reads until DA1 comes back.
// DA1 is the sentinel: terminals answer in order, so the XTVERSION reply is
// consumed before Bubble Tea starts reading stdin.
func probeTTY(f *os.File, timeout time.Duration) (string, bool) {
	if f == nil || timeout <= 0 {
		return "", false
	}
	sc, err := f.SyscallConn()
	if err != nil {
		return "", false
	}
	var old *term.State
	var rawErr error
	if err := sc.Control(func(fd uintptr) {
		old, rawErr = term.MakeRaw(int(fd))
	}); err != nil || rawErr != nil {
		return "", false
	}
	defer func() {
		_ = sc.Control(func(fd uintptr) {
			_ = term.Restore(int(fd), old)
		})
	}()
	if err := f.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", false
	}
	if _, err := f.WriteString(xtversionQuery + da1Query); err != nil {
		return "", false
	}
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 256)
	for !da1Seen(buf) {
		n, readErr := f.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if readErr != nil || len(buf) > 8192 {
			break
		}
	}
	return parseXTVersion(string(buf))
}
