package config

import (
	"encoding/json"
	"fmt"
)

var reservedSagittariusKeys = map[string]struct{}{
	"defaultModel":  {},
	"defaultModels": {},
	"defaultMode":   {},
	"modes":         {},
	"subagents":     {},
	"compression":   {},
	"tools":         {},
	"systemPrompt":  {},
	"snapshots":     {},
	"verify":        {},
	"web":           {},
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
		case "provider":
			if err := json.Unmarshal(val, &cfg.Provider); err != nil {
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
	if err := add("provider", cfg.Provider); err != nil {
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

func unmarshalUtilityConfig(raw json.RawMessage) (*SagittariusUtilityConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode utility config: %w", err)
	}
	cfg := &SagittariusUtilityConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		if key == "model" {
			if err := json.Unmarshal(val, &cfg.Model); err != nil {
				return nil, err
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

func marshalUtilityConfig(cfg *SagittariusUtilityConfig) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
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

func unmarshalSystemPromptConfig(raw json.RawMessage) (*SagittariusSystemPromptConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode systemPrompt config: %w", err)
	}
	cfg := &SagittariusSystemPromptConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "personality":
			if err := json.Unmarshal(val, &cfg.Personality); err != nil {
				return nil, err
			}
		case "variant":
			if err := json.Unmarshal(val, &cfg.Variant); err != nil {
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

func marshalSystemPromptConfig(cfg *SagittariusSystemPromptConfig) (json.RawMessage, error) {
	if cfg == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	add := func(key, v string) error {
		if v == "" {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		obj[key] = b
		return nil
	}
	if err := add("personality", cfg.Personality); err != nil {
		return nil, err
	}
	if err := add("variant", cfg.Variant); err != nil {
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

func unmarshalSnapshotConfig(raw json.RawMessage) (*SagittariusSnapshotConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode snapshots config: %w", err)
	}
	cfg := &SagittariusSnapshotConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "enabled":
			if err := json.Unmarshal(val, &cfg.Enabled); err != nil {
				return nil, err
			}
		case "maxFileBytes":
			if err := json.Unmarshal(val, &cfg.MaxFileBytes); err != nil {
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

func marshalSnapshotConfig(cfg *SagittariusSnapshotConfig) (json.RawMessage, error) {
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
	if err := add("enabled", cfg.Enabled); err != nil {
		return nil, err
	}
	if err := add("maxFileBytes", cfg.MaxFileBytes); err != nil {
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

func unmarshalVerifyConfig(raw json.RawMessage) (*SagittariusVerifyConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode verify config: %w", err)
	}
	cfg := &SagittariusVerifyConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "suggestAfterWrite":
			if err := json.Unmarshal(val, &cfg.SuggestAfterWrite); err != nil {
				return nil, err
			}
		case "allowFix":
			if err := json.Unmarshal(val, &cfg.AllowFix); err != nil {
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

func marshalVerifyConfig(cfg *SagittariusVerifyConfig) (json.RawMessage, error) {
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
	if err := add("suggestAfterWrite", cfg.SuggestAfterWrite); err != nil {
		return nil, err
	}
	if err := add("allowFix", cfg.AllowFix); err != nil {
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

func unmarshalWebConfig(raw json.RawMessage) (*SagittariusWebConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("decode web config: %w", err)
	}
	cfg := &SagittariusWebConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "searchEnabled":
			if err := json.Unmarshal(val, &cfg.SearchEnabled); err != nil {
				return nil, err
			}
		case "fetchEnabled":
			if err := json.Unmarshal(val, &cfg.FetchEnabled); err != nil {
				return nil, err
			}
		case "directWebFetch":
			if err := json.Unmarshal(val, &cfg.DirectWebFetch); err != nil {
				return nil, err
			}
		case "utilityModel":
			if err := json.Unmarshal(val, &cfg.UtilityModel); err != nil {
				return nil, err
			}
		case "retryFetchErrors":
			if err := json.Unmarshal(val, &cfg.RetryFetchErrors); err != nil {
				return nil, err
			}
		case "maxFetchBytes":
			if err := json.Unmarshal(val, &cfg.MaxFetchBytes); err != nil {
				return nil, err
			}
		default:
			cfg.Extra[key] = val
		}
	}
	return cfg, nil
}

func marshalWebConfig(cfg *SagittariusWebConfig) (json.RawMessage, error) {
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
	if err := add("searchEnabled", cfg.SearchEnabled); err != nil {
		return nil, err
	}
	if err := add("fetchEnabled", cfg.FetchEnabled); err != nil {
		return nil, err
	}
	if err := add("directWebFetch", cfg.DirectWebFetch); err != nil {
		return nil, err
	}
	if err := add("utilityModel", cfg.UtilityModel); err != nil {
		return nil, err
	}
	if err := add("retryFetchErrors", cfg.RetryFetchErrors); err != nil {
		return nil, err
	}
	if err := add("maxFetchBytes", cfg.MaxFetchBytes); err != nil {
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
		case "defaultModels":
			if err := json.Unmarshal(val, &s.DefaultModels); err != nil {
				return nil, fmt.Errorf("decode sagittarius.defaultModels: %w", err)
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
		case "compression":
			u, err := unmarshalUtilityConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.compression: %w", err)
			}
			s.Compression = u
		case "tools":
			u, err := unmarshalUtilityConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.tools: %w", err)
			}
			s.Tools = u
		case "systemPrompt":
			sp, err := unmarshalSystemPromptConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.systemPrompt: %w", err)
			}
			s.SystemPrompt = sp
		case "snapshots":
			snap, err := unmarshalSnapshotConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.snapshots: %w", err)
			}
			s.Snapshots = snap
		case "verify":
			v, err := unmarshalVerifyConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.verify: %w", err)
			}
			s.Verify = v
		case "web":
			w, err := unmarshalWebConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.web: %w", err)
			}
			s.Web = w
		case "goal":
			g, err := unmarshalGoalConfig(val)
			if err != nil {
				return nil, fmt.Errorf("decode sagittarius.goal: %w", err)
			}
			s.Goal = g
		case "maxToolRounds":
			var n int
			if err := json.Unmarshal(val, &n); err != nil {
				return nil, fmt.Errorf("decode sagittarius.maxToolRounds: %w", err)
			}
			s.MaxToolRounds = &n
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
	if err := add("defaultModels", s.DefaultModels); err != nil {
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
	if s.Compression != nil {
		b, err := marshalUtilityConfig(s.Compression)
		if err != nil {
			return nil, err
		}
		obj["compression"] = b
	}
	if s.Tools != nil {
		b, err := marshalUtilityConfig(s.Tools)
		if err != nil {
			return nil, err
		}
		obj["tools"] = b
	}
	if s.SystemPrompt != nil {
		b, err := marshalSystemPromptConfig(s.SystemPrompt)
		if err != nil {
			return nil, err
		}
		obj["systemPrompt"] = b
	}
	if s.Snapshots != nil {
		b, err := marshalSnapshotConfig(s.Snapshots)
		if err != nil {
			return nil, err
		}
		obj["snapshots"] = b
	}
	if s.Verify != nil {
		b, err := marshalVerifyConfig(s.Verify)
		if err != nil {
			return nil, err
		}
		obj["verify"] = b
	}
	if s.Web != nil {
		b, err := marshalWebConfig(s.Web)
		if err != nil {
			return nil, err
		}
		obj["web"] = b
	}
	if s.Goal != nil {
		b, err := marshalGoalConfig(s.Goal)
		if err != nil {
			return nil, err
		}
		obj["goal"] = b
	}
	if err := add("maxToolRounds", s.MaxToolRounds); err != nil {
		return nil, err
	}
	for key, val := range s.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}

func unmarshalGoalConfig(raw json.RawMessage) (*SagittariusGoalConfig, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	c := &SagittariusGoalConfig{Extra: make(map[string]json.RawMessage)}
	for key, val := range obj {
		switch key {
		case "maxTurns":
			var n int
			if err := json.Unmarshal(val, &n); err != nil {
				return nil, err
			}
			c.MaxTurns = &n
		case "evaluatorProvider":
			if err := json.Unmarshal(val, &c.EvaluatorProvider); err != nil {
				return nil, err
			}
		case "evaluatorModel":
			if err := json.Unmarshal(val, &c.EvaluatorModel); err != nil {
				return nil, err
			}
		case "evaluatorTimeout":
			var n int
			if err := json.Unmarshal(val, &n); err != nil {
				return nil, err
			}
			c.EvaluatorTimeout = &n
		case "defaultBudget":
			var n int
			if err := json.Unmarshal(val, &n); err != nil {
				return nil, err
			}
			c.DefaultBudget = &n
		default:
			c.Extra[key] = val
		}
	}
	if len(c.Extra) == 0 {
		c.Extra = nil
	}
	return c, nil
}

func marshalGoalConfig(c *SagittariusGoalConfig) (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	obj := make(map[string]json.RawMessage)
	if c.MaxTurns != nil {
		b, err := json.Marshal(*c.MaxTurns)
		if err != nil {
			return nil, err
		}
		obj["maxTurns"] = b
	}
	if c.EvaluatorProvider != "" {
		b, err := json.Marshal(c.EvaluatorProvider)
		if err != nil {
			return nil, err
		}
		obj["evaluatorProvider"] = b
	}
	if c.EvaluatorModel != "" {
		b, err := json.Marshal(c.EvaluatorModel)
		if err != nil {
			return nil, err
		}
		obj["evaluatorModel"] = b
	}
	if c.EvaluatorTimeout != nil {
		b, err := json.Marshal(*c.EvaluatorTimeout)
		if err != nil {
			return nil, err
		}
		obj["evaluatorTimeout"] = b
	}
	if c.DefaultBudget != nil {
		b, err := json.Marshal(*c.DefaultBudget)
		if err != nil {
			return nil, err
		}
		obj["defaultBudget"] = b
	}
	for key, val := range c.Extra {
		obj[key] = val
	}
	if len(obj) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(obj)
}
