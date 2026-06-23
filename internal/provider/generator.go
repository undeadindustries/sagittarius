package provider

import "context"

// ContentGenerator streams model output for a single generation request.
type ContentGenerator interface {
	GenerateContentStream(ctx context.Context, req *GenerateRequest) (<-chan StreamResponse, error)
}

// WireRequestDebugger is an optional interface implemented by generators that
// own their request serialization (openai-chat, openai-responses). It returns
// the exact wire body the generator would POST for req, as indented JSON, so
// /chat debug can dump the real payload. Generators that delegate marshalling to
// a third-party SDK (e.g. the Gemini genai client) do not implement it; the
// caller falls back to the provider-neutral GenerateRequest in that case.
type WireRequestDebugger interface {
	DebugWireRequest(req *GenerateRequest) ([]byte, error)
}
