package provider

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestComposeCustomProviderBaseURL(t *testing.T) {
	tests := []struct {
		name      string
		hostOrURL string
		port      string
		want      string
		wantErr   bool
	}{
		{
			name:      "bare host uses default port 8000",
			hostOrURL: "127.0.0.1",
			port:      "",
			want:      "http://127.0.0.1:8000",
		},
		{
			name:      "bare host with explicit port",
			hostOrURL: "localhost",
			port:      "11434",
			want:      "http://localhost:11434",
		},
		{
			name:      "full URL with port passes through",
			hostOrURL: "http://127.0.0.1:8000",
			port:      "",
			want:      "http://127.0.0.1:8000",
		},
		{
			name:      "full URL without port receives port from field",
			hostOrURL: "http://myllm.example.com",
			port:      "9000",
			want:      "http://myllm.example.com:9000",
		},
		{
			name:      "full URL with port ignores port field",
			hostOrURL: "http://127.0.0.1:8000/v1",
			port:      "9999",
			want:      "http://127.0.0.1:8000/v1",
		},
		{
			name:      "https URL accepted",
			hostOrURL: "https://api.example.com/v1",
			port:      "",
			want:      "https://api.example.com/v1",
		},
		{
			name:      "empty hostOrURL returns error",
			hostOrURL: "",
			port:      "",
			wantErr:   true,
		},
		{
			name:      "non-http scheme returns error",
			hostOrURL: "ftp://example.com",
			port:      "",
			wantErr:   true,
		},
		{
			name:      "URL missing host returns error",
			hostOrURL: "http://",
			port:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ComposeCustomProviderBaseURL(tt.hostOrURL, tt.port)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ComposeCustomProviderBaseURL(%q, %q) error = %v, wantErr %v",
					tt.hostOrURL, tt.port, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("ComposeCustomProviderBaseURL(%q, %q) = %q, want %q",
					tt.hostOrURL, tt.port, got, tt.want)
			}
		})
	}
}

func TestParseCustomProviderEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		baseURL   string
		wantHost  string
		wantPort  string
		wantNoErr bool
	}{
		{
			name:      "URL with port splits cleanly",
			baseURL:   "http://127.0.0.1:8000",
			wantHost:  "http://127.0.0.1",
			wantPort:  "8000",
			wantNoErr: true,
		},
		{
			name:      "URL without port returns empty port",
			baseURL:   "http://myllm.example.com/v1",
			wantHost:  "http://myllm.example.com/v1",
			wantPort:  "",
			wantNoErr: true,
		},
		{
			name:      "URL with path and port",
			baseURL:   "http://127.0.0.1:8000/v1",
			wantHost:  "http://127.0.0.1/v1",
			wantPort:  "8000",
			wantNoErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotPort, err := ParseCustomProviderEndpoint(tt.baseURL)
			if tt.wantNoErr && err != nil {
				t.Fatalf("ParseCustomProviderEndpoint(%q) unexpected error: %v", tt.baseURL, err)
			}
			if gotPort != tt.wantPort {
				t.Errorf("port = %q, want %q", gotPort, tt.wantPort)
			}
			if gotHost != tt.wantHost {
				t.Errorf("host = %q, want %q", gotHost, tt.wantHost)
			}
		})
	}
}

func TestValidateCustomProviderBaseURL(t *testing.T) {
	tests := []struct {
		url     string
		wantErr bool
	}{
		{"http://127.0.0.1:8000", false},
		{"https://api.openai.com/v1", false},
		{"http://localhost:11434/v1", false},
		{"", true},
		{"ftp://example.com", true},
		{"not-a-url", true},
		{"http://", true},
	}

	for _, tt := range tests {
		err := ValidateCustomProviderBaseURL(tt.url)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateCustomProviderBaseURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
		}
	}
}

func TestCustomIDFromBaseURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"http://127.0.0.1:8000", "127-0-0-1-8000"},
		{"http://localhost:11434", "localhost-11434"},
		{"https://api.openai.com/v1", "api-openai-com"},
		{"not-a-url", "custom"},
		{"", "custom"},
	}

	for _, tt := range tests {
		got := CustomIDFromBaseURL(tt.raw)
		if got != tt.want {
			t.Errorf("CustomIDFromBaseURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestClaimCustomProviderIDCollision(t *testing.T) {
	settings := settingsWithCustomIDs("127-0-0-1-8000", "127-0-0-1-8000-1")
	id := ClaimCustomProviderID(settings, "http://127.0.0.1:8000")
	if id != "127-0-0-1-8000-2" {
		t.Errorf("ClaimCustomProviderID collision = %q, want 127-0-0-1-8000-2", id)
	}
}

// settingsWithCustomIDs builds a minimal Settings with the given custom provider ids.
func settingsWithCustomIDs(ids ...string) *config.Settings {
	custom := make(map[string]config.CustomProviderDefinition, len(ids))
	for _, id := range ids {
		custom[id] = config.CustomProviderDefinition{
			DisplayName: id,
			BaseURL:     "http://placeholder",
			WireFormat:  config.WireFormatOpenAIChat,
		}
	}
	return &config.Settings{
		Providers: &config.ProvidersSettings{Custom: custom},
	}
}
