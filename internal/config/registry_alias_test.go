package config

import "testing"

func TestNormalizeProviderIDGeminiAlias(t *testing.T) {
	t.Parallel()
	if got := NormalizeProviderID("gemini"); got != string(BuiltInGeminiAPIKey) {
		t.Fatalf("NormalizeProviderID(gemini) = %q, want %q", got, BuiltInGeminiAPIKey)
	}
	if got := NormalizeProviderID("GEMINI"); got != string(BuiltInGeminiAPIKey) {
		t.Fatalf("NormalizeProviderID(GEMINI) = %q", got)
	}
}

func TestProviderDisplayIDGemini(t *testing.T) {
	t.Parallel()
	if got := ProviderDisplayID(string(BuiltInGeminiAPIKey)); got != "gemini" {
		t.Fatalf("ProviderDisplayID = %q, want gemini", got)
	}
	if got := ProviderDisplayID("openai"); got != "openai" {
		t.Fatalf("ProviderDisplayID(openai) = %q", got)
	}
}

func TestLookupBuiltInProviderAcceptsGeminiAlias(t *testing.T) {
	t.Parallel()
	_, ok := LookupBuiltInProvider("gemini")
	if !ok {
		t.Fatal("LookupBuiltInProvider(gemini) = false")
	}
}
