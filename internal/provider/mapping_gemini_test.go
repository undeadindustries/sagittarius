package provider

import (
	"bytes"
	"testing"
)

// TestThoughtSignatureRoundTrip verifies a model functionCall part keeps its
// Gemini thought signature through the provider->genai->provider conversion.
func TestThoughtSignatureRoundTrip(t *testing.T) {
	t.Parallel()

	sig := []byte("sig-abc-123")
	in := []Part{{
		FunctionCall:     &ToolCall{Name: "list_directory", Args: map[string]any{"path": "."}},
		ThoughtSignature: sig,
	}}

	genaiParts := partsToGenai(in)
	if len(genaiParts) != 1 {
		t.Fatalf("partsToGenai len = %d, want 1", len(genaiParts))
	}
	if !bytes.Equal(genaiParts[0].ThoughtSignature, sig) {
		t.Fatalf("genai part signature = %q, want %q", genaiParts[0].ThoughtSignature, sig)
	}

	back := GenaiPartsToParts(genaiParts)
	if len(back) != 1 || back[0].FunctionCall == nil {
		t.Fatalf("round-trip parts = %#v", back)
	}
	if !bytes.Equal(back[0].ThoughtSignature, sig) {
		t.Fatalf("round-trip signature = %q, want %q", back[0].ThoughtSignature, sig)
	}
}

// TestThoughtSignatureParallelCalls verifies that for parallel function calls
// only the first part carries a signature (Gemini's contract) and both calls
// survive the conversion.
func TestThoughtSignatureParallelCalls(t *testing.T) {
	t.Parallel()

	sig := []byte("first-call-sig")
	in := []Part{
		{
			FunctionCall:     &ToolCall{Name: "list_directory", Args: map[string]any{"path": "a"}},
			ThoughtSignature: sig,
		},
		{
			FunctionCall: &ToolCall{Name: "list_directory", Args: map[string]any{"path": "b"}},
		},
	}

	back := GenaiPartsToParts(partsToGenai(in))
	if len(back) != 2 {
		t.Fatalf("parts len = %d, want 2", len(back))
	}
	if !bytes.Equal(back[0].ThoughtSignature, sig) {
		t.Errorf("first signature = %q, want %q", back[0].ThoughtSignature, sig)
	}
	if len(back[1].ThoughtSignature) != 0 {
		t.Errorf("second signature = %q, want empty", back[1].ThoughtSignature)
	}
	if back[0].FunctionCall.Args["path"] != "a" || back[1].FunctionCall.Args["path"] != "b" {
		t.Errorf("call args not preserved: %#v / %#v", back[0].FunctionCall, back[1].FunctionCall)
	}
}

// TestThoughtSignatureMultiStepHistory verifies a multi-step tool-calling
// history (user -> model+FC+sig -> user+FR -> model+FC+sig) serializes to
// genai contents without dropping either signature.
func TestThoughtSignatureMultiStepHistory(t *testing.T) {
	t.Parallel()

	sig1 := []byte("step1-sig")
	sig2 := []byte("step2-sig")
	history := []Message{
		{Role: RoleUser, Parts: []Part{{Text: "list both dirs"}}},
		{Role: RoleModel, Parts: []Part{{
			FunctionCall:     &ToolCall{Name: "list_directory", Args: map[string]any{"path": "a"}},
			ThoughtSignature: sig1,
		}}},
		{Role: RoleUser, Parts: []Part{{FunctionResponse: &FunctionResponse{
			Name:     "list_directory",
			Response: map[string]any{"entries": []any{"x"}},
		}}}},
		{Role: RoleModel, Parts: []Part{{
			FunctionCall:     &ToolCall{Name: "list_directory", Args: map[string]any{"path": "b"}},
			ThoughtSignature: sig2,
		}}},
	}

	contents := MessagesToGenaiContents(history)
	if len(contents) != 4 {
		t.Fatalf("contents len = %d, want 4", len(contents))
	}
	if got := contents[1].Parts[0].ThoughtSignature; !bytes.Equal(got, sig1) {
		t.Errorf("step1 signature = %q, want %q", got, sig1)
	}
	if got := contents[3].Parts[0].ThoughtSignature; !bytes.Equal(got, sig2) {
		t.Errorf("step2 signature = %q, want %q", got, sig2)
	}
}
