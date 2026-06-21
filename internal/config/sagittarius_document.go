package config

import (
	"encoding/json"
	"fmt"
)

var reservedSagittariusKeys = map[string]struct{}{
	"defaultModel": {},
	"defaultMode":  {},
	"modes":        {},
	"subagents":    {},
}

var reservedSagittariusModeKeys = map[string]struct{}{
	"plan":  {},
	"ask":   {},
	"debug": {},
	"agent": {},
}

var reservedSagittariusSubagentKeys = map[string]struct{}{
	"default": {},
}

func unmarshalModeConfig(raw json.RawMessage) (*SagittariusModeConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode mode config: %w", err)
	}
	cfg := &SagittariusModeConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "model":
			if err := json.Unmarshal(val, &cfg.Model); err != nil {
				return nil, err
			}
		case "systemPromptSuffix":
			if err := json.Unmarshal(val, &cfg.SystemPromptSuffix); err != nil {
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

func marshalModeConfig(cfg *SagittariusModeConfig) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	add := func(key string, v any) error {
		if isEmptyValue(v) {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj[key] = b
		return nil
	}
	if err := add("model", cfg.Model); err != nil {
		return nil, err
	}
	if err := add("systemPromptSuffix", cfg.SystemPromptSuffix); err != nil {
		return nil, err
	}
	for key, val := range cfg.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalModes(raw json.RawMessage) (*SagittariusModes, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode sagittarius.modes: %w", err)
	}
	m := &SagittariusModes{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		if _, reserved := reservedSagittariusModeKeys[key]; reserved {
			cfg, err := unmarshalModeConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.modes.%s: %w", key, err)
			}
			switch key {
			case "plan":
				m.Plan = cfg
			case "ask":
				m.Ask = cfg
			case "debug":
				m.Debug = cfg
			case "agent":
				m.Agent = cfg
			}
			continue
		}
		m.Extra[key] = val
	}
	if len(m.Extra) == 0 {
		m.Extra = nil
	}
	return m, nil
}

func marshalModes(m *SagittariusModes) (json.RawMessage, error) {
	if m == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	add := func(key string, cfg *SagittariusModeConfig) error {
		if cfg == nil {
			return nil
		}
		b, err := marshalModeConfig(cfg)
		if err != nil {
			return err
		}
		obj[key] = b
		return nil
	}
	if err := add("plan", m.Plan); err != nil {
		return nil, err
	}
	if err := add("ask", m.Ask); err != nil {
		return nil, err
	}
	if err := add("debug", m.Debug); err != nil {
		return nil, err
	}
	if err := add("agent", m.Agent); err != nil {
		return nil, err
	}
	for key, val := range m.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalSubagentConfig(raw json.RawMessage) (SagittariusSubagentConfig, error) {
	var cfg SagittariusSubagentConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return cfg, fmt.Errorf("decode subagent config: %w", err)
	}
	cfg.Extra = make(map[string]json.RawMessage)
	for key, val := range obj {
		if key == "model" {
			if err := json.Unmarshal(val, &cfg.Model); err != nil {
				return cfg, err
			}
			continue
		}
		cfg.Extra[key] = val
	}
	if len(cfg.Extra) == 0 {
		cfg.Extra = nil
	}
	return cfg, nil
}

func marshalSubagentConfig(cfg SagittariusSubagentConfig) (json.RawMessage, error) {
	obj := make(map[string]json.RawMessage)
	if cfg.Model != "" {
		b, err := json.Marshal(cfg.Model)
		if err != nil {
			return nil, err
		}
		obj["model"] = b
	}
	for key, val := range cfg.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalSubagents(raw json.RawMessage) (*SagittariusSubagents, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode sagittarius.subagents: %w", err)
	}
	s := &SagittariusSubagents{
		Named: make(map[string]SagittariusSubagentConfig),
		Extra: make(map[string]json.RawMessage),
	}
	for key, val := range obj {
		if key == "default" {
			cfg, err := unmarshalSubagentConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.subagents.default: %w", err)
			}
			s.Default = cfg
			continue
		}
		if _, reserved := reservedSagittariusSubagentKeys[key]; reserved {
			continue
		}
		cfg, err := unmarshalSubagentConfig(val)
		if err != nil {
			return nil, fmt.Errorf("decode sagittarius.subagents.%s: %w", key, err)
		}
		s.Named[key] = cfg
	}
	if len(s.Named) == 0 {
		s.Named = nil
	}
	if len(s.Extra) == 0 {
		s.Extra = nil
	}
	return s, nil
}

func marshalSubagents(s *SagittariusSubagents) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	if s.Default.Model != "" || len(s.Default.Extra) > 0 {
		b, err := marshalSubagentConfig(s.Default)
		if err != nil {
			return nil, err
		}
		obj["default"] = b
	}
	for name, cfg := range s.Named {
		b, err := marshalSubagentConfig(cfg)
		if err != nil {
			return nil, err
		}
		obj[name] = b
	}
	for key, val := range s.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalSagittarius(raw json.RawMessage) (*SagittariusSettings, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode sagittarius section: %w", err)
	}
	s := &SagittariusSettings{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "defaultModel":
			if err := json.Unmarshal(val, &s.DefaultModel); err != nil {
				return nil, fmt.Errorf("decode sagittarius.defaultModel: %w", err)
			}
		case "defaultMode":
			if err := json.Unmarshal(val, &s.DefaultMode); err != nil {
				return nil, fmt.Errorf("decode sagittarius.defaultMode: %w", err)
			}
		case "modes":
			m, err := unmarshalModes(val)
			if err != nil {
				return nil, err
			}
			s.Modes = m
		case "subagents":
			sub, err := unmarshalSubagents(val)
			if err != nil {
				return nil, err
			}
			s.Subagents = sub
		default:
			if _, reserved := reservedSagittariusKeys[key]; reserved {
				continue
			}
			s.Extra[key] = val
		}
	}
	if len(s.Extra) == 0 {
		s.Extra = nil
	}
	if err := ValidateSagittariusSettings(s); err != nil {
		return nil, err
	}
	return s, nil
}

func marshalSagittarius(s *SagittariusSettings) (json.RawMessage, error) {
	if s == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	add := func(key string, v any) error {
		if isEmptyValue(v) {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj[key] = b
		return nil
	}
	if err := add("defaultModel", s.DefaultModel); err != nil {
		return nil, err
	}
	if err := add("defaultMode", s.DefaultMode); err != nil {
		return nil, err
	}
	if s.Modes != nil {
		b, err := marshalModes(s.Modes)
		if err != nil {
			return nil, err
		}
		obj["modes"] = b
	}
	if s.Subagents != nil {
		b, err := marshalSubagents(s.Subagents)
		if err != nil {
			return nil, err
		}
		obj["subagents"] = b
	}
	for key, val := range s.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}
