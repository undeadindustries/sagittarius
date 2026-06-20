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
