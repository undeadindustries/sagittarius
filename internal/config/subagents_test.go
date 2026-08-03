package config

import (
	"testing"
)

func TestSubagentsConfig(t *testing.T) {
	// 1. Valid enabled
	raw := []byte(`{"enabled": true}`)
	s, err := unmarshalSubagents(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Enabled == nil || *s.Enabled != true {
		t.Errorf("expected Enabled to be true, got %v", s.Enabled)
	}

	// Round trip
	b, err := marshalSubagents(s)
	if err != nil {
		t.Fatalf("unexpected error marshaling: %v", err)
	}
	if string(b) != `{"enabled":true}` {
		t.Errorf("unexpected marshaled result: %s", b)
	}

	// 2. Default and named
	raw2 := []byte(`{"default": {"model": "foo"}, "researcher": {"model": "bar"}}`)
	s2, err := unmarshalSubagents(raw2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s2.Default.Model != "foo" {
		t.Errorf("expected default model to be foo")
	}
	if s2.Named["researcher"].Model != "bar" {
		t.Errorf("expected named researcher model to be bar")
	}

	// 3. Invalid enabled (wrong type)
	raw3 := []byte(`{"enabled": {"not": "a bool"}}`)
	_, err = unmarshalSubagents(raw3)
	if err == nil {
		t.Errorf("expected error decoding invalid enabled")
	}
}
