package config

import (
	"encoding/json"
	"testing"
)

func TestResolveGoogleChat(t *testing.T) {
	t.Run("defaults when nil", func(t *testing.T) {
		got := ResolveGoogleChat(nil, nil)
		if got.Enabled {
			t.Error("expected Enabled = false by default")
		}
		if got.MaxResultRunes != DefaultGoogleChatMaxResultRunes {
			t.Errorf("got MaxResultRunes %d, want %d", got.MaxResultRunes, DefaultGoogleChatMaxResultRunes)
		}
		if got.ConfirmTimeout != DefaultGoogleChatConfirmTimeout {
			t.Errorf("got ConfirmTimeout %d, want %d", got.ConfirmTimeout, DefaultGoogleChatConfirmTimeout)
		}
	})

	t.Run("project overrides global", func(t *testing.T) {
		gEnabled := true
		pEnabled := false
		gMaxRunes := 1000
		pMaxRunes := 4000
		gTimeout := 60
		pTimeout := 120

		global := &Settings{
			Sagittarius: &SagittariusSettings{
				Chat: &SagittariusChatConfig{
					GoogleChat: &SagittariusGoogleChatConfig{
						Enabled:         &gEnabled,
						SpaceID:         "spaces/GLOBAL",
						AuthorizedUsers: []string{"users/g1"},
						ProjectID:       "proj-g",
						SubscriptionID:  "sub-g",
						CredentialsFile: "/path/global.json",
						MaxResultRunes:  &gMaxRunes,
						ConfirmTimeout:  &gTimeout,
					},
				},
			},
		}

		project := &Settings{
			Sagittarius: &SagittariusSettings{
				Chat: &SagittariusChatConfig{
					GoogleChat: &SagittariusGoogleChatConfig{
						Enabled:         &pEnabled,
						SpaceID:         "spaces/PROJECT",
						AuthorizedUsers: []string{"users/p1", "rob@undeadindustries.com"},
						ProjectID:       "proj-p",
						SubscriptionID:  "sub-p",
						CredentialsFile: "/path/project.json",
						MaxResultRunes:  &pMaxRunes,
						ConfirmTimeout:  &pTimeout,
					},
				},
			},
		}

		got := ResolveGoogleChat(global, project)
		if got.Enabled != false {
			t.Errorf("got Enabled %v, want false", got.Enabled)
		}
		if got.SpaceID != "spaces/PROJECT" {
			t.Errorf("got SpaceID %q, want spaces/PROJECT", got.SpaceID)
		}
		if len(got.AuthorizedUsers) != 2 || got.AuthorizedUsers[0] != "users/p1" {
			t.Errorf("got AuthorizedUsers %v", got.AuthorizedUsers)
		}
		if got.ProjectID != "proj-p" {
			t.Errorf("got ProjectID %q, want proj-p", got.ProjectID)
		}
		if got.SubscriptionID != "sub-p" {
			t.Errorf("got SubscriptionID %q, want sub-p", got.SubscriptionID)
		}
		if got.CredentialsFile != "/path/project.json" {
			t.Errorf("got CredentialsFile %q, want /path/project.json", got.CredentialsFile)
		}
		if got.MaxResultRunes != 4000 {
			t.Errorf("got MaxResultRunes %d, want 4000", got.MaxResultRunes)
		}
		if got.ConfirmTimeout != 120 {
			t.Errorf("got ConfirmTimeout %d, want 120", got.ConfirmTimeout)
		}
	})
}

func TestGoogleChatRoundTrip(t *testing.T) {
	raw := `{
		"chat": {
			"googleChat": {
				"enabled": true,
				"spaceId": "spaces/DM123",
				"authorizedUsers": ["users/123", "rob@undeadindustries.com"],
				"projectId": "my-gcp-project",
				"subscriptionId": "chat-sub",
				"credentialsFile": "/etc/service-account.json",
				"maxResultRunes": 3000,
				"confirmTimeout": 180
			}
		}
	}`

	cfg, err := unmarshalSagittarius(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if cfg.Chat == nil || cfg.Chat.GoogleChat == nil {
		t.Fatalf("expected Chat.GoogleChat to be populated")
	}
	gc := cfg.Chat.GoogleChat
	if gc.Enabled == nil || !*gc.Enabled {
		t.Error("expected enabled=true")
	}
	if gc.SpaceID != "spaces/DM123" {
		t.Errorf("got spaceId %q", gc.SpaceID)
	}
	if len(gc.AuthorizedUsers) != 2 {
		t.Errorf("got %d authorized users", len(gc.AuthorizedUsers))
	}
	if gc.ProjectID != "my-gcp-project" {
		t.Errorf("got projectId %q", gc.ProjectID)
	}
	if gc.SubscriptionID != "chat-sub" {
		t.Errorf("got subscriptionId %q", gc.SubscriptionID)
	}
	if gc.CredentialsFile != "/etc/service-account.json" {
		t.Errorf("got credentialsFile %q", gc.CredentialsFile)
	}
	if gc.MaxResultRunes == nil || *gc.MaxResultRunes != 3000 {
		t.Errorf("got maxResultRunes %v", gc.MaxResultRunes)
	}
	if gc.ConfirmTimeout == nil || *gc.ConfirmTimeout != 180 {
		t.Errorf("got confirmTimeout %v", gc.ConfirmTimeout)
	}

	marshaled, err := marshalSagittarius(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var roundTripped map[string]any
	if err := json.Unmarshal(marshaled, &roundTripped); err != nil {
		t.Fatalf("unmarshal marshaled: %v", err)
	}
	chatMap, ok := roundTripped["chat"].(map[string]any)
	if !ok {
		t.Fatalf("missing chat in marshaled json: %s", string(marshaled))
	}
	gcMap, ok := chatMap["googleChat"].(map[string]any)
	if !ok {
		t.Fatalf("missing googleChat in marshaled json: %s", string(marshaled))
	}
	if gcMap["spaceId"] != "spaces/DM123" {
		t.Errorf("round-tripped spaceId = %v, want spaces/DM123", gcMap["spaceId"])
	}
}
