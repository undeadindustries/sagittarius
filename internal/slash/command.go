package slash

// Command describes one slash command or subcommand entry in the registry.
type Command struct {
	Name        string
	Description string
	Hidden      bool
	SubCommands []Command
	// Handler executes the command when no deeper subcommand matches.
	// Leaf commands and parent commands with takesArgs semantics use this.
	Handler func(ctx *Context) Result
	// ArgComplete returns argument-value candidates for this command's first
	// argument (e.g. provider ids for `/provider use`). It is used by the TUI
	// completer only and must be fast and read-only. argPrefix is the partial
	// token already typed (empty after a trailing space). A nil ArgComplete
	// means the command takes no completable arguments.
	ArgComplete func(deps Deps, argPrefix string) []string
}

// MaxDescriptionLen is the fork limit for user-visible command descriptions.
const MaxDescriptionLen = 100
