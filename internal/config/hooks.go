package config

import (
	"encoding/json"
)

// HooksConfig holds the top-level "hooks" settings object.
type HooksConfig struct {
	Enabled       *bool                      `json:"enabled,omitempty"`
	Disabled      []string                   `json:"disabled,omitempty"`
	Notifications []string                   `json:"notifications,omitempty"`
	Events        map[string][]HookDefConfig `json:"-"`
	Raw           map[string]json.RawMessage `json:"-"`
}

// HookDefConfig groups hook configurations under a matcher.
type HookDefConfig struct {
	Matcher    string           `json:"matcher,omitempty"`
	Sequential bool             `json:"sequential,omitempty"`
	Hooks      []HookExecConfig `json:"hooks"`
}

// HookExecConfig holds configuration for an individual hook command.
type HookExecConfig struct {
	Type        string            `json:"type"`
	Command     string            `json:"command,omitempty"`
	Name        string            `json:"name,omitempty"`
	Description string            `json:"description,omitempty"`
	Timeout     int               `json:"timeout,omitempty"` // seconds, matching gemini-cli and Claude Code
	Source      string            `json:"source,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// GetHooks retrieves and unmarshals top-level "hooks" from Settings.Raw.
func (s *Settings) GetHooks() *HooksConfig {
	if s == nil || s.Raw == nil {
		return nil
	}
	raw, ok := s.Raw["hooks"]
	if !ok || len(raw) == 0 {
		return nil
	}
	return ParseHooksConfig(raw)
}

// SetHooks marshals and updates top-level "hooks" in Settings.Raw.
func (s *Settings) SetHooks(cfg *HooksConfig) error {
	if s == nil {
		return nil
	}
	if s.Raw == nil {
		s.Raw = make(map[string]json.RawMessage)
	}
	if cfg == nil {
		delete(s.Raw, "hooks")
		return nil
	}
	b, err := MarshalHooksConfig(cfg)
	if err != nil {
		return err
	}
	s.Raw["hooks"] = b
	return nil
}

// ParseHooksConfig unmarshals raw JSON into a HooksConfig struct.
func ParseHooksConfig(raw []byte) *HooksConfig {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	cfg := &HooksConfig{
		Events: make(map[string][]HookDefConfig),
		Raw:    top,
	}
	for k, v := range top {
		switch k {
		case "enabled":
			var b bool
			if err := json.Unmarshal(v, &b); err == nil {
				cfg.Enabled = &b
			}
		case "disabled":
			var arr []string
			if err := json.Unmarshal(v, &arr); err == nil {
				cfg.Disabled = arr
			}
		case "notifications":
			var arr []string
			if err := json.Unmarshal(v, &arr); err == nil {
				cfg.Notifications = arr
			}
		default:
			var defs []HookDefConfig
			if err := json.Unmarshal(v, &defs); err == nil {
				cfg.Events[k] = defs
			}
		}
	}
	return cfg
}

// MarshalHooksConfig marshals HooksConfig back to JSON bytes.
func MarshalHooksConfig(cfg *HooksConfig) ([]byte, error) {
	if cfg == nil {
		return []byte("{}"), nil
	}
	out := make(map[string]any)
	for k, v := range cfg.Raw {
		out[k] = v
	}
	if cfg.Enabled != nil {
		out["enabled"] = *cfg.Enabled
	}
	if len(cfg.Disabled) > 0 {
		out["disabled"] = cfg.Disabled
	}
	if len(cfg.Notifications) > 0 {
		out["notifications"] = cfg.Notifications
	}
	for evt, defs := range cfg.Events {
		out[evt] = defs
	}
	return json.Marshal(out)
}
