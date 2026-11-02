package systempromptdialog

import "context"

// Deps performs the side effects the system-prompt picker needs. Every method
// except CurrentPresetID may touch disk or rebuild the runner, so the dialog
// calls them only from tea.Cmd goroutines.
type Deps interface {
	// CurrentPresetID returns the active selection: a built-in preset id (e.g.
	// "programmer-lite") or a custom prompt name.
	CurrentPresetID() string
	// ApplyPreset selects a built-in preset id or a custom prompt name, saves
	// it to settings, and reloads the runner.
	ApplyPreset(ctx context.Context, presetID string) (string, error)

	// ListCustomPrompts returns saved custom prompt names, sorted.
	ListCustomPrompts() ([]string, error)
	// LoadCustomPrompt returns a saved prompt's text for editing.
	LoadCustomPrompt(name string) (string, error)
	// SaveCustomPrompt stores body under name. With create set it fails when
	// the name is taken. Returns a confirmation line.
	SaveCustomPrompt(ctx context.Context, name, body string, create bool) (string, error)
	// ImportCustomPrompt copies the text of the file at path into name.
	ImportCustomPrompt(ctx context.Context, name, path string, create bool) (string, error)
	// RenameCustomPrompt renames a prompt and updates selections that use it.
	RenameCustomPrompt(ctx context.Context, oldName, newName string) (string, error)
	// DeleteCustomPrompt removes a prompt and clears selections that use it.
	DeleteCustomPrompt(ctx context.Context, name string) (string, error)
}
