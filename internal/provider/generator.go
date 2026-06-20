package provider

import "context"

// ContentGenerator streams model output for a single generation request.
type ContentGenerator interface {
	GenerateContentStream(ctx context.Context, req *GenerateRequest) (<-chan StreamResponse, error)
}
