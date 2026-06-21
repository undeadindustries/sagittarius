package contextmgmt

import "testing"

func TestAssessTurnBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         PreTurnBudgetInput
		wantCompress  bool
		wantTokens    int
		checkPositive bool
	}{
		{
			name: "well below trigger does not compress",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   5_000,
				EstimatedRequestTokens: 500,
				ContextLimit:           32_768,
				ReservedResponseTokens: 4_096,
				ProactiveCompressAt:    0.8,
			},
			wantCompress:  false,
			wantTokens:    9_596,
			checkPositive: true,
		},
		{
			name: "meeting trigger compresses",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   22_300,
				EstimatedRequestTokens: 100,
				ContextLimit:           32_768,
				ReservedResponseTokens: 4_096,
				ProactiveCompressAt:    0.8,
			},
			wantCompress: true,
			wantTokens:   26_496,
		},
		{
			name: "non-positive context limit does not compress",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   100_000,
				EstimatedRequestTokens: 100_000,
				ContextLimit:           0,
				ReservedResponseTokens: 4_096,
				ProactiveCompressAt:    0.8,
			},
			wantCompress: false,
			wantTokens:   0,
		},
		{
			name: "trigger above 1 clamps to 1 but still fires when over limit",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   100_000,
				EstimatedRequestTokens: 100_000,
				ContextLimit:           32_768,
				ReservedResponseTokens: 4_096,
				ProactiveCompressAt:    5,
			},
			wantCompress: true,
			wantTokens:   204_096,
		},
		{
			name: "negative inputs treated as zero",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   -50,
				EstimatedRequestTokens: -10,
				ContextLimit:           32_768,
				ReservedResponseTokens: -5,
				ProactiveCompressAt:    0.8,
			},
			wantCompress: false,
			wantTokens:   0,
		},
		{
			name: "trigger below 0 clamps to 0 and always fires",
			input: PreTurnBudgetInput{
				CurrentHistoryTokens:   0,
				EstimatedRequestTokens: 0,
				ContextLimit:           32_768,
				ReservedResponseTokens: 0,
				ProactiveCompressAt:    -1,
			},
			wantCompress: true,
			wantTokens:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := AssessTurnBudget(tt.input)
			if got.ShouldCompressFirst != tt.wantCompress {
				t.Errorf("ShouldCompressFirst = %v, want %v", got.ShouldCompressFirst, tt.wantCompress)
			}
			if got.ProjectedTokens != tt.wantTokens {
				t.Errorf("ProjectedTokens = %d, want %d", got.ProjectedTokens, tt.wantTokens)
			}
			if tt.checkPositive && got.ProjectedFraction <= 0 {
				t.Errorf("ProjectedFraction = %v, want > 0", got.ProjectedFraction)
			}
		})
	}
}
