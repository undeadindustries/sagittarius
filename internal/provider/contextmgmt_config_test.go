package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestResolveContextManagementGating proves the context-management defenses are
// enabled only for the openai-chat wire format. Gemini-native and
// openai-responses paths must report Enabled=false so the agent builds no
// manager and never masks or compresses client-side (AD-014/AD-015).
func TestResolveContextManagementGating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		active      string
		wantEnabled bool
	}{
		{"openai-chat is enabled", string(config.BuiltInOpenAI), true},
		{"gemini-native is not masked", string(config.BuiltInGeminiAPIKey), false},
		{"openai-responses is not masked", string(config.BuiltInOpenAIResponses), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			settings := &config.Settings{
				Providers: &config.ProvidersSettings{Active: tt.active},
			}
			cm := ResolveContextManagement(settings)
			if cm.Enabled != tt.wantEnabled {
				t.Fatalf("Enabled = %v, want %v", cm.Enabled, tt.wantEnabled)
			}
		})
	}
}

func TestResolveContextManagementDefaults(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI)},
	}
	cm := ResolveContextManagement(settings)

	if cm.ContextLimit != DefaultLocalContextLimit {
		t.Errorf("ContextLimit = %d, want %d", cm.ContextLimit, DefaultLocalContextLimit)
	}
	if !cm.MaskingEnabled {
		t.Error("MaskingEnabled should default to true")
	}
	if !cm.MaskingProtectLatestTurn {
		t.Error("MaskingProtectLatestTurn should default to true")
	}
	if cm.CompressionThresholdUserSet {
		t.Error("CompressionThresholdUserSet should be false when unset")
	}
}

func TestResolveContextManagementHonorsOverrides(t *testing.T) {
	t.Parallel()

	limit := 16_000
	threshold := 0.55
	maskingOff := false
	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: string(config.BuiltInOpenAI),
			OpenAI: &config.ProviderInstanceConfig{
				ContextLimit:             &limit,
				CompressionThreshold:     &threshold,
				ToolOutputMaskingEnabled: &maskingOff,
			},
		},
	}
	cm := ResolveContextManagement(settings)

	if cm.ContextLimit != limit {
		t.Errorf("ContextLimit = %d, want %d", cm.ContextLimit, limit)
	}
	if cm.CompressionThreshold != threshold || !cm.CompressionThresholdUserSet {
		t.Errorf("threshold = %v set=%v, want %v true", cm.CompressionThreshold, cm.CompressionThresholdUserSet, threshold)
	}
	if cm.MaskingEnabled {
		t.Error("MaskingEnabled should reflect the explicit false override")
	}
}
