package agent

import "testing"

func TestProviderModelLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		provider string
		model    string
		want     string
	}{
		{"openrouter", "qwen/qwen3.7-plus", "openrouter - qwen/qwen3.7-plus"},
		{"gemini", "gemini-3.1-pro-preview", "gemini - gemini-3.1-pro-preview"},
		{"", "gpt-5-codex", "gpt-5-codex"},
		{"openai", "", "openai"},
		{"  openrouter  ", "  qwen  ", "openrouter - qwen"},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := providerModelLabel(tc.provider, tc.model); got != tc.want {
			t.Errorf("providerModelLabel(%q, %q) = %q, want %q", tc.provider, tc.model, got, tc.want)
		}
	}
}
