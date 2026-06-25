package provider

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

type fakeStreamer struct {
	chunks []streamChunk
}

type streamChunk struct {
	resp *genai.GenerateContentResponse
	err  error
}

func (f fakeStreamer) GenerateContentStream(
	_ context.Context,
	_ string,
	_ []*genai.Content,
	_ *genai.GenerateContentConfig,
) iter.Seq2[*genai.GenerateContentResponse, error] {
	return func(yield func(*genai.GenerateContentResponse, error) bool) {
		for _, chunk := range f.chunks {
			if !yield(chunk.resp, chunk.err) {
				return
			}
		}
	}
}

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{values: map[string]string{}}
}

func (m *memoryStore) Get(_ context.Context, account string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.values[account], nil
}

func (m *memoryStore) Set(_ context.Context, account, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[account] = value
	return nil
}

func (m *memoryStore) Delete(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, account)
	return nil
}

func (m *memoryStore) Available(context.Context) bool { return true }

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func withEmptyCredentials(t *testing.T) {
	t.Helper()
	// Serialize against any sibling test that touches credential globals, and
	// hold the lock across the ResetForTesting cleanup below (registered after,
	// so it runs before the unlock under t.Cleanup's LIFO order). Callers must
	// therefore not be parallel.
	t.Cleanup(credentials.LockTestGlobals())
	credentials.SetStoreFactoryForTesting(func(string) credentials.Store {
		return newMemoryStore()
	})
	t.Cleanup(credentials.ResetForTesting)
}

func textResponse(text string) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Parts: []*genai.Part{{Text: text}},
			},
		}},
	}
}

func toolCallResponse(name string, args map[string]any) *genai.GenerateContentResponse {
	return &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Parts: []*genai.Part{{
					FunctionCall: &genai.FunctionCall{
						Name: name,
						Args: args,
					},
				}},
			},
		}},
	}
}

func collectStream(t *testing.T, ch <-chan StreamResponse) []StreamResponse {
	t.Helper()
	var out []StreamResponse
	for chunk := range ch {
		out = append(out, chunk)
	}
	return out
}

func TestGeminiStreamTextDelta(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		chunks   []streamChunk
		wantText string
		wantDone bool
		wantErr  bool
	}{
		{
			name: "single text delta",
			chunks: []streamChunk{
				{resp: textResponse("Hello")},
				{resp: textResponse(" world")},
			},
			wantText: "Hello world",
			wantDone: true,
		},
		{
			name: "stream error",
			chunks: []streamChunk{
				{resp: textResponse("partial")},
				{err: genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "quota exceeded"}},
			},
			wantText: "partial",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gen, err := NewGeminiGenerator(testContext(t), GeminiConfig{
				Model:    "gemini-2.5-pro",
				Streamer: fakeStreamer{chunks: tt.chunks},
			})
			if err != nil {
				t.Fatalf("NewGeminiGenerator: %v", err)
			}

			ch, err := gen.GenerateContentStream(testContext(t), &GenerateRequest{
				Messages: []Message{{Role: RoleUser, Parts: []Part{{Text: "hi"}}}},
			})
			if err != nil {
				t.Fatalf("GenerateContentStream: %v", err)
			}

			chunks := collectStream(t, ch)
			var gotText string
			var gotDone bool
			var gotErr error
			for _, chunk := range chunks {
				gotText += chunk.TextDelta
				if chunk.Done {
					gotDone = true
				}
				if chunk.Error != nil {
					gotErr = chunk.Error
				}
			}

			if gotText != tt.wantText {
				t.Errorf("text = %q, want %q", gotText, tt.wantText)
			}
			if gotDone != tt.wantDone {
				t.Errorf("done = %v, want %v", gotDone, tt.wantDone)
			}
			if tt.wantErr {
				if gotErr == nil {
					t.Fatal("expected stream error, got nil")
				}
				if !errors.Is(gotErr, ErrQuotaExceeded) {
					t.Errorf("error = %v, want ErrQuotaExceeded", gotErr)
				}
			} else if gotErr != nil {
				t.Errorf("unexpected error: %v", gotErr)
			}
		})
	}
}

func TestGeminiInvalidKey(t *testing.T) {
	// Not parallel: t.Setenv and withEmptyCredentials mutate process-global state
	// (environment + credential store factory); a parallel sibling could observe
	// or clobber it mid-resolution.
	ctx := testContext(t)
	withEmptyCredentials(t)
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("GOOGLE_API_KEY", "")

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{Active: geminiProviderID},
	}

	_, err := NewContentGenerator(ctx, settings)
	if err == nil {
		t.Fatal("expected error for missing api key")
	}
	if !errors.Is(err, credentials.ErrAPIKeyMissing) {
		t.Errorf("error = %v, want ErrAPIKeyMissing", err)
	}
}

func TestFactorySelectsGeminiAPIKey(t *testing.T) {
	// Not parallel: t.Setenv mutates the process environment.
	ctx := testContext(t)
	t.Setenv("GEMINI_API_KEY", "test-key")

	settings := &config.Settings{
		Providers: &config.ProvidersSettings{
			Active: geminiProviderID,
			GeminiAPIKey: &config.ProviderInstanceConfig{
				Model: "gemini-2.5-pro",
			},
		},
	}

	gen, err := NewContentGenerator(ctx, settings)
	if err != nil {
		t.Fatalf("NewContentGenerator: %v", err)
	}
	if _, ok := gen.(*GeminiGenerator); !ok {
		t.Fatalf("generator type = %T, want *GeminiGenerator", gen)
	}
}

func TestToolCallRoundTrip(t *testing.T) {
	t.Parallel()

	decl := ToolDeclaration{
		Name:        "get_weather",
		Description: "Returns weather for a city",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"city": map[string]any{"type": "string"},
			},
			"required": []any{"city"},
		},
	}

	tools := ToolDeclarationsToGenai([]ToolDeclaration{decl})
	if len(tools) != 1 || len(tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %#v, want one function declaration", tools)
	}
	gotDecl := tools[0].FunctionDeclarations[0]
	if gotDecl.Name != "get_weather" {
		t.Errorf("name = %q, want get_weather", gotDecl.Name)
	}

	resp := toolCallResponse("get_weather", map[string]any{"city": "Paris"})
	calls := ToolCallsFromGenaiResponse(resp)
	if len(calls) != 1 {
		t.Fatalf("calls = %#v, want one call", calls)
	}
	if calls[0].Name != "get_weather" {
		t.Errorf("call name = %q, want get_weather", calls[0].Name)
	}
	if calls[0].Args["city"] != "Paris" {
		t.Errorf("call args[city] = %v, want Paris", calls[0].Args["city"])
	}

	msg := Message{
		Role: RoleModel,
		Parts: []Part{{
			FunctionCall: &ToolCall{
				Name: "get_weather",
				Args: map[string]any{"city": "Paris"},
			},
		}},
	}
	contents := MessagesToGenaiContents([]Message{msg})
	if len(contents) != 1 {
		t.Fatalf("contents len = %d, want 1", len(contents))
	}
	roundTrip := GenaiPartsToParts(contents[0].Parts)
	if len(roundTrip) != 1 || roundTrip[0].FunctionCall == nil {
		t.Fatalf("roundTrip = %#v", roundTrip)
	}
	if roundTrip[0].FunctionCall.Name != "get_weather" {
		t.Errorf("roundTrip name = %q, want get_weather", roundTrip[0].FunctionCall.Name)
	}
}

func TestMapAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantIs  error
		contain string
	}{
		{
			name:   "invalid key 401",
			err:    genai.APIError{Code: 401, Status: "UNAUTHENTICATED", Message: "API key not valid"},
			wantIs: ErrInvalidAPIKey,
		},
		{
			name:   "model not found 404",
			err:    genai.APIError{Code: 404, Status: "NOT_FOUND", Message: "models/bad-model not found"},
			wantIs: ErrModelNotFound,
		},
		{
			name:   "quota 429",
			err:    genai.APIError{Code: 429, Status: "RESOURCE_EXHAUSTED", Message: "Quota exceeded"},
			wantIs: ErrQuotaExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mapped := MapAPIError(tt.err)
			if !errors.Is(mapped, tt.wantIs) {
				t.Errorf("MapAPIError() = %v, want Is(%v)", mapped, tt.wantIs)
			}
		})
	}
}
