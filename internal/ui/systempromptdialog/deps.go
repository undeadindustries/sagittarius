package systempromptdialog

import "context"

// Deps performs the side effects the project system-prompt picker needs.
type Deps interface {
	// CurrentPresetID returns the active project preset id (e.g. "programmer-lite").
	CurrentPresetID() string
	// ApplyPreset writes the preset to the project settings and reloads the runner.
	ApplyPreset(ctx context.Context, presetID string) (string, error)
}
