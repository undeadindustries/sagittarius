package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Screen-layout values for ui.screen. Empty means fullscreen, the default
// everywhere including SSH. auto is the opt-in that uses inline scrollback
// over SSH unless the terminal is known to allow OSC 52, and full screen locally.
const (
	UIScreenAuto       = "auto"
	UIScreenFullscreen = "fullscreen"
	UIScreenInline     = "inline"
)

// ValidUIScreens is the /settings choice list for ui.screen. fullscreen is
// first because it is the default.
var ValidUIScreens = []string{UIScreenFullscreen, UIScreenInline, UIScreenAuto}

// ParseUIScreen normalizes a ui.screen value. Empty means fullscreen. Anything
// else that is not auto or inline is an error.
func ParseUIScreen(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", UIScreenFullscreen:
		return UIScreenFullscreen, nil
	case UIScreenAuto:
		return UIScreenAuto, nil
	case UIScreenInline:
		return UIScreenInline, nil
	default:
		return "", fmt.Errorf("ui.screen must be fullscreen, inline, or auto")
	}
}

// UISettings is the typed view of the passthrough "ui" section of settings.json.
// Only the keys Sagittarius consumes are modeled; unknown ui.* keys round-trip
// untouched via Settings.Raw.
type UISettings struct {
	// Theme selects the color theme ("default" or "greyscale"). Empty means the
	// default purple theme. Resolved against NO_COLOR in the bubbletea layer.
	Theme string `json:"theme,omitempty"`
	// Screen selects the terminal layout: fullscreen, inline, or auto. Empty
	// means fullscreen. A change applies on the next launch. /screen changes
	// the running session without writing this key.
	Screen string `json:"screen,omitempty"`
	// Mouse is the persisted in-app mouse preference for full-screen sessions.
	// Nil means the default (on). A false value starts the next full-screen
	// session with the terminal's own selection.
	Mouse *bool `json:"mouse,omitempty"`
	// HideBanner suppresses the ASCII launch banner when true.
	HideBanner bool `json:"hideBanner,omitempty"`
	// HideTips suppresses the welcome tips block when true.
	HideTips bool `json:"hideTips,omitempty"`
	// ShowThinking reveals the model reasoning ("thinking") box by default.
	// Off by default; toggled live with Ctrl+T or per-provider/model settings.
	ShowThinking bool `json:"showThinking,omitempty"`
	// EscapeAtOnPaste automatically escapes unescaped '@' characters as '\@'
	// when pasting text into the composer, preventing diffs, emails, and
	// decorators from triggering atmention file expansion.
	EscapeAtOnPaste bool `json:"escapeAtOnPaste,omitempty"`
	// ToolkitChecklistDismissed marks whether the user has permanently dismissed the host toolkit checklist.
	ToolkitChecklistDismissed bool `json:"toolkitChecklistDismissed,omitempty"`
	// SSHCopyTipShown records that the one-time "copying over SSH" dialog has
	// been offered. It is not a layout preference and does not appear in /settings.
	SSHCopyTipShown bool `json:"sshCopyTipShown,omitempty"`
	// UpdateCheckedAt is the RFC3339 timestamp of the last background update
	// check, used to throttle checks to at most once per day. Global-only cache,
	// not a user preference.
	UpdateCheckedAt string `json:"updateCheckedAt,omitempty"`
}

// UI returns the parsed ui.* section, or the zero value when absent or invalid.
// A malformed ui block is treated as unset rather than a hard error so a typo
// never blocks startup.
func (s *Settings) UI() UISettings {
	var ui UISettings
	if s == nil || s.Raw == nil {
		return ui
	}
	raw, ok := s.Raw["ui"]
	if !ok {
		return ui
	}
	_ = json.Unmarshal(raw, &ui)
	ui.Theme = strings.TrimSpace(ui.Theme)
	ui.Screen = strings.ToLower(strings.TrimSpace(ui.Screen))
	return ui
}

// mutateUISection loads the passthrough ui.* object, applies mutate, and stores
// the result (removing the section entirely when it ends up empty). Unknown
// ui.* keys round-trip untouched. The caller flushes via Loader.Save.
func (s *Settings) mutateUISection(mutate func(map[string]json.RawMessage) error) error {
	if s == nil {
		return fmt.Errorf("mutate ui section: nil settings")
	}
	if s.Raw == nil {
		s.Raw = make(map[string]json.RawMessage)
	}
	ui := make(map[string]json.RawMessage)
	if raw, ok := s.Raw["ui"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &ui); err != nil {
			return fmt.Errorf("decode ui section: %w", err)
		}
	}
	if err := mutate(ui); err != nil {
		return err
	}
	if len(ui) == 0 {
		delete(s.Raw, "ui")
		return nil
	}
	b, err := json.Marshal(ui)
	if err != nil {
		return fmt.Errorf("encode ui section: %w", err)
	}
	s.Raw["ui"] = b
	return nil
}

// SetUITheme records the TUI color theme under ui.theme, preserving other ui.*
// keys. An empty or "default" name clears the key (default is the implicit
// theme). The caller flushes the mutation via Loader.Save.
func (s *Settings) SetUITheme(name string) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		name = strings.TrimSpace(name)
		if name == "" || name == "default" {
			delete(ui, "theme")
			return nil
		}
		encoded, err := json.Marshal(name)
		if err != nil {
			return fmt.Errorf("encode ui theme: %w", err)
		}
		ui["theme"] = encoded
		return nil
	})
}

// SetUIToolkitChecklistDismissed records the toolkit checklist dismissal state
// under ui.toolkitChecklistDismissed, preserving other ui.* keys. A false value
// clears the key (off is the implicit default). The caller flushes via Loader.Save.
func (s *Settings) SetUIToolkitChecklistDismissed(dismissed bool) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if !dismissed {
			delete(ui, "toolkitChecklistDismissed")
			return nil
		}
		ui["toolkitChecklistDismissed"] = []byte("true")
		return nil
	})
}

// SetUIUpdateCheckedAt records the last update check time under ui.updateCheckedAt.
// A zero time clears the key.
func (s *Settings) SetUIUpdateCheckedAt(t time.Time) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if t.IsZero() {
			delete(ui, "updateCheckedAt")
			return nil
		}
		// Format as RFC3339 string
		encoded, err := json.Marshal(t.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("encode ui updateCheckedAt: %w", err)
		}
		ui["updateCheckedAt"] = encoded
		return nil
	})
}

// SetUIShowThinking records the global thinking-box visibility under
// ui.showThinking, preserving other ui.* keys. A false value clears the key
// (off is the implicit default). The caller flushes via Loader.Save.
func (s *Settings) SetUIShowThinking(on bool) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if !on {
			delete(ui, "showThinking")
			return nil
		}
		ui["showThinking"] = json.RawMessage("true")
		return nil
	})
}

// ClearUIField removes one key from the passthrough ui object without writing
// a false (or any other) value. The section is dropped when it becomes empty.
func (s *Settings) ClearUIField(field string) error {
	if strings.TrimSpace(field) == "" {
		return fmt.Errorf("clear ui field: empty name")
	}
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		delete(ui, field)
		return nil
	})
}

// SetUIScreen records the terminal layout under ui.screen. Empty or "fullscreen"
// clears the key (fullscreen is the implicit default). "auto" is stored, because
// it is no longer the default. The caller flushes via Loader.Save.
func (s *Settings) SetUIScreen(name string) error {
	parsed, err := ParseUIScreen(name)
	if err != nil {
		return err
	}
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if parsed == UIScreenFullscreen {
			delete(ui, "screen")
			return nil
		}
		encoded, err := json.Marshal(parsed)
		if err != nil {
			return fmt.Errorf("encode ui screen: %w", err)
		}
		ui["screen"] = encoded
		return nil
	})
}

// SetUISSHCopyTipShown records that the one-time SSH copy dialog has been
// offered. False clears the key. The caller flushes via Loader.Save.
func (s *Settings) SetUISSHCopyTipShown(shown bool) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if !shown {
			delete(ui, "sshCopyTipShown")
			return nil
		}
		ui["sshCopyTipShown"] = []byte("true")
		return nil
	})
}

// SetUIMouse records whether full-screen sessions start with in-app mouse
// selection. True clears the key (on is the implicit default).
func (s *Settings) SetUIMouse(on bool) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if on {
			delete(ui, "mouse")
			return nil
		}
		ui["mouse"] = json.RawMessage("false")
		return nil
	})
}

// SetUIEscapeAtOnPaste records the paste @ escaping preference under
// ui.escapeAtOnPaste, preserving other ui.* keys. A false value clears the key
// (off is the implicit default). The caller flushes via Loader.Save.
func (s *Settings) SetUIEscapeAtOnPaste(on bool) error {
	return s.mutateUISection(func(ui map[string]json.RawMessage) error {
		if !on {
			delete(ui, "escapeAtOnPaste")
			return nil
		}
		ui["escapeAtOnPaste"] = json.RawMessage("true")
		return nil
	})
}
