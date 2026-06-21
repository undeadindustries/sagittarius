package config

import "testing"

func hasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

func TestValidSettingKeysGeminiEmpty(t *testing.T) {
	t.Parallel()
	if keys := ValidSettingKeys(WireFormatGemini); len(keys) != 0 {
		t.Fatalf("gemini ValidSettingKeys = %v, want empty", keys)
	}
}

func TestValidSettingKeysOpenAIChat(t *testing.T) {
	t.Parallel()
	keys := ValidSettingKeys(WireFormatOpenAIChat)
	for _, want := range []string{"model", "baseUrl", "temperature", "toolCallParsing", "toolOutputMaskingEnabled"} {
		if !hasKey(keys, want) {
			t.Errorf("openai-chat keys missing %q (got %v)", want, keys)
		}
	}
	if hasKey(keys, "reasoningEffort") {
		t.Error("openai-chat must not include reasoningEffort")
	}
}

func TestValidSettingKeysOpenAIResponses(t *testing.T) {
	t.Parallel()
	keys := ValidSettingKeys(WireFormatOpenAIResponses)
	for _, want := range []string{"model", "reasoningEffort", "useResponseChaining", "temperature"} {
		if !hasKey(keys, want) {
			t.Errorf("openai-responses keys missing %q (got %v)", want, keys)
		}
	}
	if hasKey(keys, "toolCallParsing") {
		t.Error("openai-responses must not include toolCallParsing")
	}
}

func TestValidSettingKeysForProviderCustomWireFormat(t *testing.T) {
	t.Parallel()
	custom := &CustomProviderDefinition{WireFormat: WireFormatOpenAIResponses}
	keys := ValidSettingKeysForProvider("my-vllm", custom)
	if !hasKey(keys, "reasoningEffort") {
		t.Fatalf("custom responses provider missing reasoningEffort (got %v)", keys)
	}

	// Built-in id resolves from the registry regardless of the custom arg.
	if keys := ValidSettingKeysForProvider("gemini", nil); len(keys) != 0 {
		t.Fatalf("gemini provider keys = %v, want empty", keys)
	}
}
