package tools

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
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

// InteractiveTool is an optional interface for tools that pose a structured
// question to the user and wait for a reply mid-execution (e.g. grill mode's
// ask_user). interactive reports whether a UI consumer is attached to answer
// emitted events; headless callers have nothing to answer them, so an
// InteractiveTool must degrade gracefully (e.g. auto-select a recommended
// default) rather than block forever when interactive is false.
type InteractiveTool interface {
	Tool
	ExecuteInteractive(ctx context.Context, args map[string]any, interactive bool, emit func(ui.StreamEvent)) (map[string]any, error)
}

// NestedToolRunner executes one nested call under the caller's gates and hooks.
type NestedToolRunner func(ctx context.Context, name string, args map[string]any) (map[string]any, error)

// BatchTool executes other tools. The scheduler supplies a runner so nested
// calls pass the same gates and lifecycle hooks as a direct invocation.
type BatchTool interface {
	Tool
	ExecuteBatch(ctx context.Context, args map[string]any, run NestedToolRunner) (map[string]any, error)
}
