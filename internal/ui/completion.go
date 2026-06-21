package ui

// Suggestion is one slash-command or argument completion candidate shown in the
// inline suggestion list beneath the input box.
type Suggestion struct {
	// Label is the text shown in the suggestion list (e.g. "provider").
	Label string
	// Description is optional dim help text shown beside the label.
	Description string
	// Insert is the token text inserted when the suggestion is accepted.
	Insert string
	// AppendSpace requests a trailing space after insertion (the command has
	// subcommands or expects an argument), so the next token can be typed and
	// completed without first pressing space.
	AppendSpace bool
}

// Completions is the result of completing a partial input line.
type Completions struct {
	// Items are the ranked candidates (empty when nothing matches).
	Items []Suggestion
	// ReplaceFrom is the byte offset in the input where the active token starts.
	// Accepting a suggestion replaces input[ReplaceFrom:] with the suggestion's
	// Insert value (plus an optional trailing space).
	ReplaceFrom int
}

// Completer optionally provides slash-command completions for the input line.
// The TUI type-asserts the App to this interface; apps that do not implement it
// simply have no autocompletion. Implementations must be fast and non-blocking
// (no network) because Complete runs on the UI thread on every keystroke.
type Completer interface {
	Complete(input string) Completions
}
