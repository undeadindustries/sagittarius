package config

import (
	"testing"
)

func TestEditEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name    string
		global  *Settings
		project *Settings
		want    bool
	}{
		{
			name:    "nil settings",
			global:  nil,
			project: nil,
			want:    true,
		},
		{
			name:    "global disabled",
			global:  &Settings{Sagittarius: &SagittariusSettings{Edit: &SagittariusEditConfig{Enabled: &disabled}}},
			project: nil,
			want:    false,
		},
		{
			name:    "global enabled, project disabled",
			global:  &Settings{Sagittarius: &SagittariusSettings{Edit: &SagittariusEditConfig{Enabled: &enabled}}},
			project: &Settings{Sagittarius: &SagittariusSettings{Edit: &SagittariusEditConfig{Enabled: &disabled}}},
			want:    false,
		},
		{
			name:    "global disabled, project enabled",
			global:  &Settings{Sagittarius: &SagittariusSettings{Edit: &SagittariusEditConfig{Enabled: &disabled}}},
			project: &Settings{Sagittarius: &SagittariusSettings{Edit: &SagittariusEditConfig{Enabled: &enabled}}},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EditEnabled(tt.global, tt.project); got != tt.want {
				t.Errorf("EditEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
