package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UISettings is the typed view of the passthrough "ui" section of settings.json.
// Only the keys Sagittarius consumes are modeled; unknown ui.* keys round-trip
// untouched via Settings.Raw.
type UISettings struct {
	// Theme selects the color theme ("default" or "greyscale"). Empty means the
	// default purple theme. Resolved against NO_COLOR in the bubbletea layer.
	Theme string `json:"theme,omitempty"`
	// HideBanner suppresses the ASCII launch banner when true.
	HideBanner bool `json:"hideBanner,omitempty"`
	// HideTips suppresses the welcome tips block when true.
	HideTips bool `json:"hideTips,omitempty"`
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
	return ui
}

// SetUITheme records the TUI color theme under ui.theme, preserving other ui.*
// keys. An empty or "default" name clears the key (default is the implicit
// theme). The caller flushes the mutation via Loader.Save.
func (s *Settings) SetUITheme(name string) error {
	if s == nil {
		return fmt.Errorf("set ui theme: nil settings")
	}
	if s.Raw == nil {
		s.Raw = make(map[string]json.RawMessage)
	}

	// Decode the existing ui.* object into a generic map so unknown keys
	// (hideBanner/hideTips and anything Sagittarius does not model) round-trip.
	ui := make(map[string]json.RawMessage)
	if raw, ok := s.Raw["ui"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &ui); err != nil {
			return fmt.Errorf("decode ui section: %w", err)
		}
	}

	name = strings.TrimSpace(name)
	if name == "" || name == "default" {
		delete(ui, "theme")
	} else {
		encoded, err := json.Marshal(name)
		if err != nil {
			return fmt.Errorf("encode ui theme: %w", err)
		}
		ui["theme"] = encoded
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
