package config

import (
	"encoding/json"
	"fmt"
)

var reservedProviderKeys = map[string]struct{}{
	"active":           {},
	"custom":           {},
	"openai":           {},
	"gemini-apikey":    {},
	"gemini-oauth":     {},
	"gemini-vertex":    {},
	"openai-responses": {},
}

func unmarshalProviderInstance(raw json.RawMessage) (*ProviderInstanceConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode provider instance: %w", err)
	}
	cfg := &ProviderInstanceConfig{Extra: make(map[string]json.RawMessage)}
	known := map[string]func(json.RawMessage) error{
		"model":        func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.Model) },
		"baseUrl":      func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.BaseURL) },
		"contextLimit": func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.ContextLimit) },
		"compressionThreshold": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.CompressionThreshold)
		},
		"preserveFraction": func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.PreserveFraction) },
		"promptMode":       func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.PromptMode) },
		"enableTools":      func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.EnableTools) },
		"timeout":          func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.Timeout) },
		"temperature":      func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.Temperature) },
		"toolCallParsing":  func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.ToolCallParsing) },
		"systemPromptOverride": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.SystemPromptOverride)
		},
		"reasoningEffort":     func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.ReasoningEffort) },
		"useResponseChaining": func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.UseResponseChaining) },
		"wireFormat":          func(b json.RawMessage) error { return json.Unmarshal(b, &cfg.WireFormat) },
		"toolOutputMaskingEnabled": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.ToolOutputMaskingEnabled)
		},
		"toolOutputMaskingProtectionFraction": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.ToolOutputMaskingProtectionFraction)
		},
		"toolOutputMaskingPrunableFraction": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.ToolOutputMaskingPrunableFraction)
		},
		"toolOutputMaskingProtectLatestTurn": func(b json.RawMessage) error {
			return json.Unmarshal(b, &cfg.ToolOutputMaskingProtectLatestTurn)
		},
	}
	for key, val := range obj {
		if decode, ok := known[key]; ok {
			if err := decode(val); err != nil {
				return nil, fmt.Errorf("decode providers field %q: %w", key, err)
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

func marshalProviderInstance(cfg *ProviderInstanceConfig) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	setField := func(key string, v any) error {
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
	fields := []struct {
		key string
		val any
	}{
		{"model", cfg.Model},
		{"baseUrl", cfg.BaseURL},
		{"contextLimit", cfg.ContextLimit},
		{"compressionThreshold", cfg.CompressionThreshold},
		{"preserveFraction", cfg.PreserveFraction},
		{"promptMode", cfg.PromptMode},
		{"enableTools", cfg.EnableTools},
		{"timeout", cfg.Timeout},
		{"temperature", cfg.Temperature},
		{"toolCallParsing", cfg.ToolCallParsing},
		{"systemPromptOverride", cfg.SystemPromptOverride},
		{"reasoningEffort", cfg.ReasoningEffort},
		{"useResponseChaining", cfg.UseResponseChaining},
		{"wireFormat", cfg.WireFormat},
		{"toolOutputMaskingEnabled", cfg.ToolOutputMaskingEnabled},
		{"toolOutputMaskingProtectionFraction", cfg.ToolOutputMaskingProtectionFraction},
		{"toolOutputMaskingPrunableFraction", cfg.ToolOutputMaskingPrunableFraction},
		{"toolOutputMaskingProtectLatestTurn", cfg.ToolOutputMaskingProtectLatestTurn},
	}
	for _, f := range fields {
		if err := setField(f.key, f.val); err != nil {
			return nil, err
		}
	}
	for key, val := range cfg.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalCustomProvider(raw json.RawMessage) (CustomProviderDefinition, error) {
	var def CustomProviderDefinition
	if len(raw) == 0 {
		return def, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return def, fmt.Errorf("decode custom provider: %w", err)
	}
	known := map[string]func(json.RawMessage) error{
		"displayName":  func(b json.RawMessage) error { return json.Unmarshal(b, &def.DisplayName) },
		"baseUrl":      func(b json.RawMessage) error { return json.Unmarshal(b, &def.BaseURL) },
		"defaultModel": func(b json.RawMessage) error { return json.Unmarshal(b, &def.DefaultModel) },
		"defaultContextLimit": func(b json.RawMessage) error {
			return json.Unmarshal(b, &def.DefaultContextLimit)
		},
		"apiKeyEnvVar": func(b json.RawMessage) error { return json.Unmarshal(b, &def.APIKeyEnvVar) },
		"wireFormat":   func(b json.RawMessage) error { return json.Unmarshal(b, &def.WireFormat) },
	}
	def.Extra = make(map[string]json.RawMessage)
	for key, val := range obj {
		if decode, ok := known[key]; ok {
			if err := decode(val); err != nil {
				return def, fmt.Errorf("decode custom provider field %q: %w", key, err)
			}
			continue
		}
		def.Extra[key] = val
	}
	if len(def.Extra) == 0 {
		def.Extra = nil
	}
	return def, nil
}

func marshalCustomProvider(def CustomProviderDefinition) (json.RawMessage, error) {
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
	if err := add("displayName", def.DisplayName); err != nil {
		return nil, err
	}
	if err := add("baseUrl", def.BaseURL); err != nil {
		return nil, err
	}
	if err := add("defaultModel", def.DefaultModel); err != nil {
		return nil, err
	}
	if err := add("defaultContextLimit", def.DefaultContextLimit); err != nil {
		return nil, err
	}
	if err := add("apiKeyEnvVar", def.APIKeyEnvVar); err != nil {
		return nil, err
	}
	if err := add("wireFormat", def.WireFormat); err != nil {
		return nil, err
	}
	for key, val := range def.Extra {
		obj[key] = val
	}
	return json.Marshal(obj)
}

func unmarshalProviders(raw json.RawMessage) (*ProvidersSettings, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode providers section: %w", err)
	}
	ps := &ProvidersSettings{Extra: make(map[string]json.RawMessage)}
	if val, ok := obj["active"]; ok {
		if err := json.Unmarshal(val, &ps.Active); err != nil {
			return nil, fmt.Errorf("decode providers.active: %w", err)
		}
	}
	if val, ok := obj["openai"]; ok {
		cfg, err := unmarshalProviderInstance(val)
		if err != nil {
			return nil, err
		}
		ps.OpenAI = cfg
	}
	if val, ok := obj["gemini-apikey"]; ok {
		cfg, err := unmarshalProviderInstance(val)
		if err != nil {
			return nil, err
		}
		ps.GeminiAPIKey = cfg
	}
	if val, ok := obj["openai-responses"]; ok {
		cfg, err := unmarshalProviderInstance(val)
		if err != nil {
			return nil, err
		}
		ps.OpenAIResponses = cfg
	}
	if val, ok := obj["custom"]; ok {
		var customObj map[string]json.RawMessage
		if err := json.Unmarshal(val, &customObj); err != nil {
			return nil, fmt.Errorf("decode providers.custom: %w", err)
		}
		ps.Custom = make(map[string]CustomProviderDefinition, len(customObj))
		for id, entry := range customObj {
			def, err := unmarshalCustomProvider(entry)
			if err != nil {
				return nil, fmt.Errorf("decode providers.custom.%s: %w", id, err)
			}
			ps.Custom[id] = def
		}
	}
	for key, val := range obj {
		if _, reserved := reservedProviderKeys[key]; reserved {
			continue
		}
		ps.Extra[key] = val
	}
	if len(ps.Extra) == 0 {
		ps.Extra = nil
	}
	return ps, nil
}

func marshalProviders(ps *ProvidersSettings) (json.RawMessage, error) {
	if ps == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	if ps.Active != "" {
		b, err := json.Marshal(ps.Active)
		if err != nil {
			return nil, err
		}
		obj["active"] = b
	}
	if ps.OpenAI != nil {
		b, err := marshalProviderInstance(ps.OpenAI)
		if err != nil {
			return nil, err
		}
		obj["openai"] = b
	}
	if ps.GeminiAPIKey != nil {
		b, err := marshalProviderInstance(ps.GeminiAPIKey)
		if err != nil {
			return nil, err
		}
		obj["gemini-apikey"] = b
	}
	if ps.OpenAIResponses != nil {
		b, err := marshalProviderInstance(ps.OpenAIResponses)
		if err != nil {
			return nil, err
		}
		obj["openai-responses"] = b
	}
	if len(ps.Custom) > 0 {
		customObj := make(map[string]json.RawMessage, len(ps.Custom))
		for id, def := range ps.Custom {
			b, err := marshalCustomProvider(def)
			if err != nil {
				return nil, err
			}
			customObj[id] = b
		}
		b, err := json.Marshal(customObj)
		if err != nil {
			return nil, err
		}
		obj["custom"] = b
	}
	for key, val := range ps.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func decodeSettingsDocument(raw []byte) (*Settings, error) {
	if len(raw) == 0 {
		return &Settings{Raw: map[string]json.RawMessage{}}, nil
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("decode settings.json: %w", err)
	}
	s := &Settings{Raw: make(map[string]json.RawMessage, len(top))}
	for key, val := range top {
		if key == "providers" {
			ps, err := unmarshalProviders(val)
			if err != nil {
				return nil, err
			}
			s.Providers = ps
			continue
		}
		s.Raw[key] = val
	}
	return s, nil
}

func encodeSettingsDocument(s *Settings) ([]byte, error) {
	if s == nil {
		return []byte("{}\n"), nil
	}
	top := make(map[string]json.RawMessage, len(s.Raw)+1)
	for key, val := range s.Raw {
		top[key] = val
	}
	if s.Providers != nil {
		b, err := marshalProviders(s.Providers)
		if err != nil {
			return nil, err
		}
		top["providers"] = b
	}
	out, err := json.MarshalIndent(top, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode settings.json: %w", err)
	}
	out = append(out, '\n')
	return out, nil
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case PromptMode:
		return t == ""
	case ToolCallParsingMode:
		return t == ""
	case WireFormat:
		return t == ""
	case *int:
		return t == nil
	case *bool:
		return t == nil
	case *float64:
		return t == nil
	default:
		return false
	}
}
