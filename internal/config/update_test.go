package config

import (
	"encoding/json"
	"testing"
)

func TestUpdateAutoCheckEnabled(t *testing.T) {
	t.Run("default is true", func(t *testing.T) {
		if got := UpdateAutoCheckEnabled(nil, nil); got != true {
			t.Errorf("expected true, got %v", got)
		}
	})

	t.Run("global override", func(t *testing.T) {
		f := false
		global := &Settings{
			Sagittarius: &SagittariusSettings{
				Update: &SagittariusUpdateConfig{
					AutoCheck: &f,
				},
			},
		}
		if got := UpdateAutoCheckEnabled(global, nil); got != false {
			t.Errorf("expected false, got %v", got)
		}
	})

	t.Run("project wins", func(t *testing.T) {
		tr := true
		f := false
		global := &Settings{
			Sagittarius: &SagittariusSettings{
				Update: &SagittariusUpdateConfig{
					AutoCheck: &f,
				},
			},
		}
		project := &Settings{
			Sagittarius: &SagittariusSettings{
				Update: &SagittariusUpdateConfig{
					AutoCheck: &tr,
				},
			},
		}
		if got := UpdateAutoCheckEnabled(global, project); got != true {
			t.Errorf("expected true, got %v", got)
		}
	})
}

func TestUpdateConfigJSON(t *testing.T) {
	data := []byte(`{"update": {"autoCheck": false, "unknownKey": "val"}}`)

	s, err := unmarshalSagittarius(data)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if s.Update == nil {
		t.Fatal("expected Update config")
	}
	if s.Update.AutoCheck == nil || *s.Update.AutoCheck != false {
		t.Errorf("expected AutoCheck=false, got %v", s.Update.AutoCheck)
	}
	if string(s.Update.Extra["unknownKey"]) != `"val"` {
		t.Errorf("expected unknownKey=val, got %s", s.Update.Extra["unknownKey"])
	}

	b, err := marshalSagittarius(s)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal back error: %v", err)
	}

	if _, ok := raw["update"]; !ok {
		t.Errorf("expected update key in marshaled output")
	}
}
