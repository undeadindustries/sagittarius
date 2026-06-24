package tools

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Tool is a built-in callable function exposed to the model.
type Tool interface {
	Name() string
	Declaration() provider.ToolDeclaration
	Execute(ctx context.Context, args map[string]any) (map[string]any, error)
	RequiresConfirmation() bool
}

// ToolOutputSink accepts live incremental output from a long-running tool.
type ToolOutputSink func(text string)

// StreamingTool is an optional interface for tools that can emit live output
// before their final Execute result.
type StreamingTool interface {
	Tool
	ExecuteStream(ctx context.Context, args map[string]any, sink ToolOutputSink) (map[string]any, error)
}
