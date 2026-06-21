package contextmgmt

import "testing"

func defaultMaskingSettings() LocalMaskingSettings {
	return LocalMaskingSettings{
		ContextLimit:       32_768,
		Enabled:            true,
		ProtectionFraction: DefaultLocalMaskingProtectionFraction,
		PrunableFraction:   DefaultLocalMaskingPrunableFraction,
		ProtectLatestTurn:  true,
	}
}

func TestGetLocalMaskingDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		mutate        func(*LocalMaskingSettings)
		wantProtect   int
		wantPrunable  int
		wantLatest    bool
		skipPrunable  bool
		skipProtect   bool
		minProtect    int
		minPrunableLo int
	}{
		{
			name:         "scales proportionally to context limit",
			mutate:       func(s *LocalMaskingSettings) { s.ContextLimit = 32_768 },
			wantProtect:  4_915, // floor(32768 * 0.15)
			wantPrunable: 3_276, // floor(32768 * 0.10)
			wantLatest:   true,
		},
		{
			name:          "enforces minimum floors for tiny limits",
			mutate:        func(s *LocalMaskingSettings) { s.ContextLimit = 1_024 },
			skipProtect:   true,
			skipPrunable:  true,
			minProtect:    minProtectionTokens,
			minPrunableLo: minPrunableTokens,
			wantLatest:    true,
		},
		{
			name: "clamps high protection fraction to 0.5",
			mutate: func(s *LocalMaskingSettings) {
				s.ContextLimit = 100_000
				s.ProtectionFraction = 5
			},
			wantProtect:  50_000,
			wantPrunable: 10_000,
			wantLatest:   true,
		},
		{
			name: "clamps low protection fraction to 0.05",
			mutate: func(s *LocalMaskingSettings) {
				s.ContextLimit = 100_000
				s.ProtectionFraction = -1
			},
			wantProtect:  5_000,
			wantPrunable: 10_000,
			wantLatest:   true,
		},
		{
			name:         "falls back to upstream defaults for non-positive limit",
			mutate:       func(s *LocalMaskingSettings) { s.ContextLimit = 0 },
			wantProtect:  DefaultToolProtectionThreshold,
			wantPrunable: DefaultMinPrunableTokensThreshold,
			wantLatest:   true,
		},
		{
			name:         "honors protectLatestTurn pass-through",
			mutate:       func(s *LocalMaskingSettings) { s.ProtectLatestTurn = false },
			wantProtect:  4_915,
			wantPrunable: 3_276,
			wantLatest:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := defaultMaskingSettings()
			tt.mutate(&cfg)
			got := GetLocalMaskingDefaults(cfg)

			if tt.skipProtect {
				if got.ProtectionThresholdTokens < tt.minProtect {
					t.Errorf("protection = %d, want >= %d", got.ProtectionThresholdTokens, tt.minProtect)
				}
			} else if got.ProtectionThresholdTokens != tt.wantProtect {
				t.Errorf("protection = %d, want %d", got.ProtectionThresholdTokens, tt.wantProtect)
			}

			if tt.skipPrunable {
				if got.MinPrunableThresholdTokens < tt.minPrunableLo {
					t.Errorf("prunable = %d, want >= %d", got.MinPrunableThresholdTokens, tt.minPrunableLo)
				}
			} else if got.MinPrunableThresholdTokens != tt.wantPrunable {
				t.Errorf("prunable = %d, want %d", got.MinPrunableThresholdTokens, tt.wantPrunable)
			}

			if got.ProtectLatestTurn != tt.wantLatest {
				t.Errorf("protectLatestTurn = %v, want %v", got.ProtectLatestTurn, tt.wantLatest)
			}
		})
	}
}
