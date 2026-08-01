package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// TestResolveContextManagementGating proves the context-management defenses are
// enabled only for the openai-chat and gemini wire formats. The
// openai-responses path must report Enabled=false so the agent builds no
// manager and never masks or compresses client-side (because it chains turns
// server-side via previous_response_id).
func TestResolveContextManagementGating(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		active       string
		wantEnabled  bool
		wantAdaptive bool
		wantFallback int
	}{
		// openai/openai-responses resolve through their AD-073 provider presets
		// (internal/config/provider_presets.go), which declare a real
		// DefaultContextLimit (128k / 400k) rather than falling back to the
		// generic DefaultLocalContextLimit placeholder.
		{"openai-chat is enabled and adaptive", string(config.BuiltInOpenAI), true, true, 128_000},
		{"gemini-native is enabled but not adaptive", string(config.BuiltInGeminiAPIKey), true, false, 1_048_576},
		{"openai-responses is not masked", string(config.BuiltInOpenAIResponses), false, false, 400_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			settings := &config.Settings{
				Providers: &config.ProvidersSettings{Active: tt.active},
			}
			cm := ResolveContextManagement(settings, "")
			if cm.Enabled != tt.wantEnabled {
				t.Fatalf("Enabled = %v, want %v", cm.Enabled, tt.wantEnabled)
			}
			if cm.Adaptive != tt.wantAdaptive {
				t.Fatalf("Adaptive = %v, want %v", cm.Adaptive, tt.wantAdaptive)
			}
			if cm.ContextLimit != tt.wantFallback {
				t.Fatalf("ContextLimit = %v, want %v", cm.ContextLimit, tt.wantFallback)
			}
		})
	}
}

func TestResolveContextManagementDefaults(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInOpenAI)},
	}
	cm := ResolveContextManagement(settings, "")

	// The "openai" id resolves through its provider preset (AD-073), which
	// declares a real 128k DefaultContextLimit; DefaultLocalContextLimit is
	// only the fallback for providers with no known preset/instance default.
	const wantContextLimit = 128_000
	if cm.ContextLimit != wantContextLimit {
		t.Errorf("ContextLimit = %d, want %d", cm.ContextLimit, wantContextLimit)
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
	cm := ResolveContextManagement(settings, "")

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

func TestResolveContextManagementGeminiFallback(t *testing.T) {
	t.Parallel()

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: string(config.BuiltInGeminiAPIKey)},
	}
	cm := ResolveContextManagement(settings, "")

	// 1,048,576 is the builtin Gemini default
	if cm.ContextLimit != 1_048_576 {
		t.Errorf("ContextLimit = %d, want 1_048_576", cm.ContextLimit)
	}
	if cm.CompressionThreshold != 0.5 {
		t.Errorf("CompressionThreshold = %v, want 0.5", cm.CompressionThreshold)
	}
	if cm.Adaptive {
		t.Error("Adaptive should be false for Gemini")
	}
}
