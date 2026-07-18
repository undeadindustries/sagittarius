package config

import (
	"encoding/json"
	"testing"
)

// TestSymbolsConfigRoundTrip guards against the symbols config being defined but
// never wired into unmarshalSagittarius/marshalSagittarius, which would silently
// drop it into Extra instead of populating SagittariusSymbolsConfig.
func TestSymbolsConfigRoundTrip(t *testing.T) {
	t.Parallel()

	s, err := unmarshalSagittarius(json.RawMessage(`{
  "symbols": {
    "enabled": false,
    "preferGopls": false
  }
}`))
	if err != nil {
		t.Fatalf("unmarshalSagittarius: %v", err)
	}
	if s.Symbols == nil {
		t.Fatal("Symbols is nil")
	}
	if s.Symbols.Enabled == nil || *s.Symbols.Enabled != false {
		t.Errorf("Enabled = %+v", s.Symbols.Enabled)
	}
	if s.Symbols.PreferGopls == nil || *s.Symbols.PreferGopls != false {
		t.Errorf("PreferGopls = %+v", s.Symbols.PreferGopls)
	}
	if _, ok := s.Extra["symbols"]; ok {
		t.Error("symbols leaked into Extra instead of being parsed into Symbols")
	}

	raw, err := marshalSagittarius(s)
	if err != nil {
		t.Fatalf("marshalSagittarius: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal serialized sagittarius: %v", err)
	}
	symRaw, ok := obj["symbols"]
	if !ok {
		t.Fatal("symbols missing from serialized output")
	}
	reparsed, err := unmarshalSymbolsConfig(symRaw)
	if err != nil {
		t.Fatalf("unmarshalSymbolsConfig(reserialized): %v", err)
	}
	if reparsed.Enabled == nil || *reparsed.Enabled != false {
		t.Errorf("reserialized Enabled = %+v", reparsed.Enabled)
	}
}

func TestSymbolsConfigExtraPassthrough(t *testing.T) {
	t.Parallel()

	cfg, err := unmarshalSymbolsConfig(json.RawMessage(`{"enabled":true,"future":"x"}`))
	if err != nil {
		t.Fatalf("unmarshalSymbolsConfig: %v", err)
	}
	if cfg.Extra["future"] == nil {
		t.Error("unknown key should round-trip via Extra")
	}
	raw, err := marshalSymbolsConfig(cfg)
	if err != nil {
		t.Fatalf("marshalSymbolsConfig: %v", err)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["future"]; !ok {
		t.Error("unknown key dropped on marshal")
	}
}

func TestSymbolsEnabledDefaults(t *testing.T) {
	t.Parallel()

	// Nil / empty documents default to on.
	if !SymbolsEnabled(nil, nil) {
		t.Error("SymbolsEnabled should default to true for nil settings")
	}
	if !SymbolsPreferGopls(nil, nil) {
		t.Error("SymbolsPreferGopls should default to true for nil settings")
	}

	empty := &Settings{Sagittarius: &SagittariusSettings{}}
	if !SymbolsEnabled(empty, nil) {
		t.Error("SymbolsEnabled should default to true when symbols is unset")
	}

	off := &Settings{Sagittarius: &SagittariusSettings{
		Symbols: &SagittariusSymbolsConfig{Enabled: boolPtr(false)},
	}}
	if SymbolsEnabled(off, nil) {
		t.Error("SymbolsEnabled should honor an explicit false")
	}

	// Project overrides global.
	globalOff := &Settings{Sagittarius: &SagittariusSettings{
		Symbols: &SagittariusSymbolsConfig{Enabled: boolPtr(false)},
	}}
	projectOn := &Settings{Sagittarius: &SagittariusSettings{
		Symbols: &SagittariusSymbolsConfig{Enabled: boolPtr(true)},
	}}
	if !SymbolsEnabled(globalOff, projectOn) {
		t.Error("project true should win over global false")
	}
}
