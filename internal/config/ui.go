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
	// ShowThinking reveals the model reasoning ("thinking") box by default.
	// Off by default; toggled live with Ctrl+T or per-provider/model settings.
	ShowThinking bool `json:"showThinking,omitempty"`
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
