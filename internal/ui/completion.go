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

// MentionCompleter optionally provides "@path" file-mention completions for an
// active mention token ending at the byte offset cursor within input. The TUI
// type-asserts the App to this interface; apps that do not implement it simply
// have no mention autocompletion. Like Completer, implementations must be fast
// and non-blocking because CompleteMention runs on the UI thread per keystroke.
type MentionCompleter interface {
	CompleteMention(input string, cursor int) Completions
}

// ToolkitChecklistMarker is the optional App capability that records the first
// display of the host toolkit checklist. The TUI type-asserts the App to this
// when the startup report renders; apps that do not implement it (tests, other
// UIs) simply never persist the shown state and the checklist follows its
// pre-flag behavior. The write is best-effort and must not block the UI thread.
type ToolkitChecklistMarker interface {
	// MarkToolkitChecklistShown records that the checklist report has been
	// displayed once, so later launches do not auto-run it again.
	MarkToolkitChecklistShown() error
}
