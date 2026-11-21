package config

import "testing"

func TestMigrateLegacyBuiltins_ActiveOpenAI(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAI)}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("expected migration to report a change")
	}
	def, ok := s.Providers.Custom[string(BuiltInOpenAI)]
	if !ok {
		t.Fatalf("custom[openai] not created: %+v", s.Providers.Custom)
	}
	if def.BaseURL != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("baseURL = %q", def.BaseURL)
	}
	if def.APIKeyEnvVar != "OPENAI_API_KEY" {
		t.Errorf("apiKeyEnvVar = %q", def.APIKeyEnvVar)
	}
	if def.WireFormat != WireFormatOpenAIChat {
		t.Errorf("wireFormat = %q", def.WireFormat)
	}
}

func TestMigrateLegacyBuiltins_Idempotent(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInOpenAI)}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("first pass should migrate")
	}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("second pass should be a no-op")
	}
}

func TestMigrateLegacyBuiltins_TypedResponsesBlock(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{
		Active:          string(BuiltInGeminiAPIKey),
		OpenAIResponses: &ProviderInstanceConfig{Model: "gpt-5-codex"},
	}}
	if !MigrateLegacyBuiltins(s) {
		t.Fatal("expected migration from typed instance block")
	}
	def, ok := s.Providers.Custom[string(BuiltInOpenAIResponses)]
	if !ok {
		t.Fatalf("custom[openai-responses] not created")
	}
	if def.WireFormat != WireFormatOpenAIResponses {
		t.Errorf("wireFormat = %q, want openai-responses", def.WireFormat)
	}
}

func TestMigrateLegacyBuiltins_NotReferenced(t *testing.T) {
	s := &Settings{Providers: &ProvidersSettings{Active: string(BuiltInGeminiAPIKey)}}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("no openai reference — should not migrate")
	}
	if _, ok := s.Providers.Custom[string(BuiltInOpenAI)]; ok {
		t.Fatal("custom[openai] should not be created when unreferenced")
	}
}

func TestMigrateLegacyBuiltins_PreservesExistingCustom(t *testing.T) {
	existing := CustomProviderDefinition{
		DisplayName: "My OpenAI",
		BaseURL:     "https://proxy.internal/v1/chat/completions",
		WireFormat:  WireFormatOpenAIChat,
	}
	s := &Settings{Providers: &ProvidersSettings{
		Active: string(BuiltInOpenAI),
		Custom: map[string]CustomProviderDefinition{string(BuiltInOpenAI): existing},
	}}
	if MigrateLegacyBuiltins(s) {
		t.Fatal("existing custom[openai] should be left untouched")
	}
	if got := s.Providers.Custom[string(BuiltInOpenAI)].BaseURL; got != existing.BaseURL {
		t.Errorf("baseURL overwritten: %q", got)
	}
}

func TestMigrateAnthropicPreset(t *testing.T) {
	t.Run("rewrites uncustomized legacy anthropic preset", func(t *testing.T) {
		oldLimit := 200_000
		s := &Settings{Providers: &ProvidersSettings{
			Active: "anthropic",
			Custom: map[string]CustomProviderDefinition{
				"anthropic": {
					DisplayName:         "Anthropic",
					BaseURL:             "https://api.anthropic.com/v1/chat/completions",
					APIKeyEnvVar:        "ANTHROPIC_API_KEY",
					WireFormat:          WireFormatOpenAIChat,
					DefaultContextLimit: &oldLimit,
				},
			},
		}}
		if !MigrateAnthropicPreset(s) {
			t.Fatal("expected migration to apply")
		}
		def := s.Providers.Custom["anthropic"]
		if def.WireFormat != WireFormatAnthropic || def.BaseURL != "https://api.anthropic.com/v1/messages" {
			t.Fatalf("migrated def = %+v", def)
		}
		if def.DefaultContextLimit == nil || *def.DefaultContextLimit != 1_000_000 {
			t.Fatalf("DefaultContextLimit = %v, want 1000000", def.DefaultContextLimit)
		}
		if MigrateAnthropicPreset(s) {
			t.Fatal("second pass should be a no-op")
		}
	})

	t.Run("skips custom user-edited anthropic URL", func(t *testing.T) {
		s := &Settings{Providers: &ProvidersSettings{
			Custom: map[string]CustomProviderDefinition{
				"anthropic": {
					DisplayName: "Anthropic Proxy",
					BaseURL:     "https://proxy.internal/v1/chat/completions",
					WireFormat:  WireFormatOpenAIChat,
				},
			},
		}}
		if MigrateAnthropicPreset(s) {
			t.Fatal("user-edited URL should not be overwritten")
		}
	})
}

func TestValidateAnthropicWorkspaceID(t *testing.T) {
	for _, valid := range []string{"", "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ"} {
		if err := ValidateAnthropicWorkspaceID(valid); err != nil {
			t.Errorf("ValidateAnthropicWorkspaceID(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"Default", "default", "workspace_123", "wrkspc_bad!"} {
		if err := ValidateAnthropicWorkspaceID(invalid); err == nil {
			t.Errorf("ValidateAnthropicWorkspaceID(%q) = nil, want error", invalid)
		}
	}
}
