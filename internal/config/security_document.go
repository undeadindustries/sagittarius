package config

import (
	"encoding/json"
	"fmt"
)

func unmarshalSecurity(raw json.RawMessage) (*SecuritySettings, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode security section: %w", err)
	}
	s := &SecuritySettings{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "projectBoundary":
			pb, err := unmarshalProjectBoundary(val)
			if err != nil {
				return nil, fmt.Errorf("decode security.projectBoundary: %w", err)
			}
			s.ProjectBoundary = pb
		default:
			s.Extra[key] = val
		}
	}
	if len(s.Extra) == 0 {
		s.Extra = nil
	}
	return s, nil
}

func marshalSecurity(s *SecuritySettings) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	if s.ProjectBoundary != nil {
		b, err := marshalProjectBoundary(s.ProjectBoundary)
		if err != nil {
			return nil, err
		}
		obj["projectBoundary"] = b
	}
	for key, val := range s.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalProjectBoundary(raw json.RawMessage) (*ProjectBoundaryConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	cfg := &ProjectBoundaryConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "enforce":
			if err := json.Unmarshal(val, &cfg.Enforce); err != nil {
				return nil, err
			}
		default:
			cfg.Extra[key] = val
		}
	}
	if len(cfg.Extra) == 0 {
		cfg.Extra = nil
	}
	return cfg, nil
}

func marshalProjectBoundary(cfg *ProjectBoundaryConfig) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	if cfg.Enforce != nil {
		b, err := json.Marshal(cfg.Enforce)
		if err != nil {
			return nil, err
		}
		obj["enforce"] = b
	}
	for key, val := range cfg.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}
