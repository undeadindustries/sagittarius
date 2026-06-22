package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// geminiLiveModel resolves the user's configured Gemini model, falling back to
// a known Gemini 3 model that enforces thought signatures.
func geminiLiveModel() string {
	const fallback = "gemini-3.1-pro-preview"
	loader, err := config.NewLoader()
	if err != nil {
		return fallback
	}
	settings, err := loader.Load()
	if err != nil || settings == nil {
		return fallback
	}
	ep, err := ResolveEndpointForProvider(settings, geminiProviderID)
	if err != nil || ep.Model == "" {
		return fallback
	}
	return ep.Model
}

// TestGeminiThoughtSignatureLive reproduces the multi-step tool-calling flow
// that triggered the 400 "missing thought_signature" error. It uses the API
// key stored by the app (env -> keychain/file store) and is skipped when no
// key is configured.
//
// Run with: go test ./internal/provider -run TestGeminiThoughtSignatureLive -v -count=1
func TestGeminiThoughtSignatureLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, err := resolveAPIKey(ctx, geminiProviderID); err != nil {
		if errors.Is(err, credentials.ErrAPIKeyMissing) {
			t.Skip("no gemini api key configured; skipping live test")
		}
		t.Fatalf("resolve gemini key: %v", err)
	}

	apiKey, err := resolveAPIKey(ctx, geminiProviderID)
	if err != nil {
		t.Fatalf("resolve gemini key: %v", err)
	}

	model := geminiLiveModel()
	t.Logf("using gemini model %q", model)

	gen, err := NewGeminiGenerator(ctx, GeminiConfig{APIKey: apiKey, Model: model})
	if err != nil {
		t.Fatalf("NewGeminiGenerator: %v", err)
	}

	tool := ToolDeclaration{
		Name:        "list_directory",
		Description: "List the entries of a directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Directory path to list.",
				},
			},
			"required": []any{"path"},
		},
	}

	// Step A: prompt that forces a tool call and capture the model parts.
	history := []Message{{
		Role:  RoleUser,
		Parts: []Part{{Text: "List the contents of the current directory using the list_directory tool. Call it for the path \".\"."}},
	}}

	modelParts := mustGenerateModelParts(t, ctx, gen, &GenerateRequest{
		Model:    model,
		Messages: history,
		Tools:    []ToolDeclaration{tool},
	})

	fcPart, hasSig := firstFunctionCallPart(modelParts)
	if fcPart == nil {
		t.Fatalf("step A: expected a functionCall part, got %#v", modelParts)
	}
	t.Logf("step A: functionCall=%q hasThoughtSignature=%v", fcPart.FunctionCall.Name, hasSig)
	if !hasSig {
		t.Fatal("step A: model functionCall part is missing a thought signature (Gemini 3 requires it)")
	}

	// Step B: replay the model parts (with signature) plus the tool result.
	history = append(history, Message{Role: RoleModel, Parts: modelParts})
	history = append(history, Message{Role: RoleUser, Parts: []Part{{
		FunctionResponse: &FunctionResponse{
			Name:     fcPart.FunctionCall.Name,
			Response: map[string]any{"entries": []any{"main.go", "README.md"}},
		},
	}}})

	// This call previously failed with 400 INVALID_ARGUMENT when the signature
	// was dropped. It must now succeed.
	if _, err := generateModelParts(ctx, gen, &GenerateRequest{
		Model:    model,
		Messages: history,
		Tools:    []ToolDeclaration{tool},
	}); err != nil {
		t.Fatalf("step B: follow-up call failed (signature likely dropped): %v", err)
	}
	t.Log("step B: follow-up call succeeded with thought signature replayed")
}

func firstFunctionCallPart(parts []Part) (*Part, bool) {
	for i := range parts {
		if parts[i].FunctionCall != nil {
			return &parts[i], len(parts[i].ThoughtSignature) > 0
		}
	}
	return nil, false
}

func mustGenerateModelParts(t *testing.T, ctx context.Context, gen ContentGenerator, req *GenerateRequest) []Part {
	t.Helper()
	parts, err := generateModelParts(ctx, gen, req)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return parts
}

// generateModelParts drains a stream and returns the final model parts, or the
// first stream error encountered.
func generateModelParts(ctx context.Context, gen ContentGenerator, req *GenerateRequest) ([]Part, error) {
	ch, err := gen.GenerateContentStream(ctx, req)
	if err != nil {
		return nil, err
	}
	var modelParts []Part
	for resp := range ch {
		if resp.Error != nil {
			return nil, resp.Error
		}
		if len(resp.ModelParts) > 0 {
			modelParts = resp.ModelParts
		}
	}
	return modelParts, nil
}
