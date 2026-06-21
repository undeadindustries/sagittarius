package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMinimalSettings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeFile(t, settingsPath, readTestdata(t, "minimal.json"))

	loader, err := NewLoader(WithSettingsPath(settingsPath))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Providers == nil {
		t.Fatal("Providers is nil")
	}
	if got, want := s.Providers.Active, "gemini-apikey"; got != want {
		t.Errorf("Active = %q, want %q", got, want)
	}
	if got, want := s.ActiveProvider(), "gemini-apikey"; got != want {
		t.Errorf("ActiveProvider() = %q, want %q", got, want)
	}
}

func TestLoadForkFixture(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeFile(t, settingsPath, readTestdata(t, "fork_fixture.json"))

	loader, err := NewLoader(WithSettingsPath(settingsPath))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name string
		fn   func(t *testing.T)
	}{
		{
			name: "active provider",
			fn: func(t *testing.T) {
				if got := s.Providers.Active; got != "openai" {
					t.Errorf("Active = %q, want openai", got)
				}
			},
		},
		{
			name: "openai overrides",
			fn: func(t *testing.T) {
				if s.Providers.OpenAI == nil {
					t.Fatal("OpenAI config is nil")
				}
				if got := s.Providers.OpenAI.Model; got != "gpt-4o-mini" {
					t.Errorf("model = %q, want gpt-4o-mini", got)
				}
				if s.Providers.OpenAI.Timeout == nil || *s.Providers.OpenAI.Timeout != 120000 {
					t.Errorf("timeout = %v, want 120000", s.Providers.OpenAI.Timeout)
				}
				if s.Providers.OpenAI.EnableTools == nil || !*s.Providers.OpenAI.EnableTools {
					t.Error("enableTools should be true")
				}
			},
		},
		{
			name: "custom provider",
			fn: func(t *testing.T) {
				def, ok := s.Providers.Custom["local-vllm"]
				if !ok {
					t.Fatal("missing custom local-vllm provider")
				}
				if def.DisplayName != "Local vLLM" {
					t.Errorf("displayName = %q", def.DisplayName)
				}
				if def.BaseURL != "http://127.0.0.1:8000/v1/chat/completions" {
					t.Errorf("baseUrl = %q", def.BaseURL)
				}
				if def.WireFormat != WireFormatOpenAIChat {
					t.Errorf("wireFormat = %q", def.WireFormat)
				}
			},
		},
		{
			name: "built-in registry",
			fn: func(t *testing.T) {
				openai, ok := LookupBuiltInProvider("openai")
				if !ok {
					t.Fatal("openai not in registry")
				}
				if openai.APIKeyEnvVar != "OPENAI_API_KEY" {
					t.Errorf("APIKeyEnvVar = %q", openai.APIKeyEnvVar)
				}
				gemini, ok := LookupBuiltInProvider("gemini-apikey")
				if !ok {
					t.Fatal("gemini-apikey not in registry")
				}
				if gemini.WireFormat != WireFormatGemini {
					t.Errorf("wireFormat = %q", gemini.WireFormat)
				}
			},
		},
		{
			name: "unknown sections preserved in raw",
			fn: func(t *testing.T) {
				if _, ok := s.Raw["ui"]; !ok {
					t.Error("ui section missing from Raw")
				}
				if _, ok := s.Raw["mcpServers"]; !ok {
					t.Error("mcpServers section missing from Raw")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.fn)
	}
}

func TestSavePreservesUnknownKeys(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeFile(t, settingsPath, readTestdata(t, "unknown_keys.json"))

	loader, err := NewLoader(WithSettingsPath(settingsPath))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s.Providers.OpenAI.Model = "gpt-4o-mini-updated"
	if err := loader.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Unmarshal saved file: %v", err)
	}

	if _, ok := doc["futureNodeOnlySection"]; !ok {
		t.Error("futureNodeOnlySection was dropped on save")
	}
	if _, ok := doc["ui"]; !ok {
		t.Error("ui section was dropped on save")
	}

	var providers map[string]json.RawMessage
	if err := json.Unmarshal(doc["providers"], &providers); err != nil {
		t.Fatalf("decode providers: %v", err)
	}
	var openai map[string]string
	if err := json.Unmarshal(providers["openai"], &openai); err != nil {
		t.Fatalf("decode openai: %v", err)
	}
	if openai["model"] != "gpt-4o-mini-updated" {
		t.Errorf("model = %q, want gpt-4o-mini-updated", openai["model"])
	}
}

func TestActiveModelsRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeFile(t, settingsPath, readTestdata(t, "minimal.json"))

	loader, err := NewLoader(WithSettingsPath(settingsPath))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	s.Providers.OpenAI = &ProviderInstanceConfig{
		Model:        "gpt-4o",
		ActiveModels: []string{"gpt-4o", "gpt-4o-mini"},
	}
	if err := loader.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := loader.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Providers.OpenAI == nil {
		t.Fatal("openai instance dropped on reload")
	}
	got := reloaded.Providers.OpenAI.ActiveModels
	if len(got) != 2 || got[0] != "gpt-4o" || got[1] != "gpt-4o-mini" {
		t.Fatalf("activeModels = %v, want [gpt-4o gpt-4o-mini]", got)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal saved file: %v", err)
	}
	var openai map[string]json.RawMessage
	if err := json.Unmarshal(doc.Providers["openai"], &openai); err != nil {
		t.Fatalf("decode openai: %v", err)
	}
	if _, ok := openai["activeModels"]; !ok {
		t.Error("activeModels missing from serialized openai block")
	}
}

func TestActiveModelsOmittedWhenEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	writeFile(t, settingsPath, readTestdata(t, "minimal.json"))

	loader, err := NewLoader(WithSettingsPath(settingsPath))
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}
	s, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.Providers.OpenAI = &ProviderInstanceConfig{Model: "gpt-4o"}
	if err := loader.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var doc struct {
		Providers map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal saved file: %v", err)
	}
	var openai map[string]json.RawMessage
	if err := json.Unmarshal(doc.Providers["openai"], &openai); err != nil {
		t.Fatalf("decode openai: %v", err)
	}
	if _, ok := openai["activeModels"]; ok {
		t.Error("empty activeModels should be omitted from serialized output")
	}
}

func TestRejectSecretsInJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name: "providers.openai.apiKey stripped on load",
			json: `{
				"providers": {
					"active": "openai",
					"openai": {
						"model": "gpt-4o",
						"apiKey": "sk-test-secret-should-not-persist"
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "providers.openai.key stripped on load",
			json: `{
				"providers": {
					"openai": { "key": "secret-value" }
				}
			}`,
			wantErr: true,
		},
		{
			name: "apiKeyEnvVar allowed on custom provider",
			json: `{
				"providers": {
					"custom": {
						"groq": {
							"displayName": "Groq",
							"baseUrl": "https://api.groq.com/v1/chat/completions",
							"apiKeyEnvVar": "GROQ_API_KEY"
						}
					}
				}
			}`,
			wantErr: false,
		},
		{
			name: "save rejects secret fields",
			json: `{
				"providers": {
					"openai": { "model": "gpt-4o" }
				}
			}`,
			wantErr: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			settingsPath := filepath.Join(dir, "settings.json")
			writeFile(t, settingsPath, []byte(tc.json))

			loader, err := NewLoader(WithSettingsPath(settingsPath))
			if err != nil {
				t.Fatalf("NewLoader: %v", err)
			}

			s, err := loader.Load()
			if tc.wantErr {
				if !errors.Is(err, ErrSecretsInSettings) {
					t.Fatalf("Load err = %v, want ErrSecretsInSettings", err)
				}
			} else if err != nil {
				t.Fatalf("Load: %v", err)
			}

			if s != nil && s.Providers != nil && s.Providers.OpenAI != nil {
				if s.Providers.OpenAI.Extra != nil {
					if _, ok := s.Providers.OpenAI.Extra["apiKey"]; ok {
						t.Error("apiKey survived in OpenAI Extra")
					}
					if _, ok := s.Providers.OpenAI.Extra["key"]; ok {
						t.Error("key survived in OpenAI Extra")
					}
				}
			}

			// Attempt to inject secret into struct extra and verify Save rejects.
			if s == nil {
				s = &Settings{Providers: &ProvidersSettings{}, Raw: map[string]json.RawMessage{}}
			}
			if s.Providers == nil {
				s.Providers = &ProvidersSettings{}
			}
			if s.Providers.OpenAI == nil {
				s.Providers.OpenAI = &ProviderInstanceConfig{}
			}
			s.Providers.OpenAI.Extra = map[string]json.RawMessage{
				"apiKey": json.RawMessage(`"inline-secret"`),
			}
			if err := loader.Save(s); !errors.Is(err, ErrSecretsInSettings) {
				t.Errorf("Save with inline secret: err = %v, want ErrSecretsInSettings", err)
			}
		})
	}
}

func TestResolveSagittariusDir(t *testing.T) {
	t.Run("default home", func(t *testing.T) {
		dir, err := ResolveSagittariusDir()
		if err != nil {
			t.Fatalf("ResolveSagittariusDir: %v", err)
		}
		if filepath.Base(dir) != ".sagittarius" {
			t.Errorf("dir = %q, want suffix .sagittarius", dir)
		}
	})

	t.Run("SAGITTARIUS_HOME override", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("SAGITTARIUS_HOME", home)
		dir, err := ResolveSagittariusDir()
		if err != nil {
			t.Fatalf("ResolveSagittariusDir: %v", err)
		}
		if dir != filepath.Join(home, ".sagittarius") {
			t.Errorf("dir = %q, want %q", dir, filepath.Join(home, ".sagittarius"))
		}
	})
}

func TestActiveProviderEnvOverride(t *testing.T) {
	t.Setenv("GEMINI_PROVIDER", "openai")
	s := &Settings{Providers: &ProvidersSettings{Active: "gemini-apikey"}}
	if got := s.ActiveProvider(); got != "openai" {
		t.Errorf("ActiveProvider() = %q, want openai", got)
	}
}

func TestMigrateLegacyLocalSettings(t *testing.T) {
	t.Parallel()

	rawLocal, _ := json.Marshal(map[string]any{
		"url":   "http://127.0.0.1:8000/v1/chat/completions",
		"model": "example-model",
	})
	s := &Settings{
		Raw: map[string]json.RawMessage{
			"local": rawLocal,
		},
	}
	result := MigrateLegacyLocalSettings(s)
	if !result.Migrated {
		t.Fatal("expected migration")
	}
	if _, ok := s.Raw["local"]; ok {
		t.Error("local block should be removed")
	}
	if s.Providers.Active != "local-vllm" {
		t.Errorf("active = %q, want local-vllm", s.Providers.Active)
	}
}

func TestReloadNotifier(t *testing.T) {
	n := NewReloadNotifier()
	ch := n.Subscribe()
	if ch == nil {
		t.Fatal("Subscribe returned nil channel")
	}
	called := false
	n.OnReload(func() { called = true })
	n.Notify()
	select {
	case <-ch:
	default:
		t.Error("expected reload signal on channel")
	}
	if !called {
		t.Error("OnReload callback not invoked")
	}
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
