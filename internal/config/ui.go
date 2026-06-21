package config

import (
	"encoding/json"
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
