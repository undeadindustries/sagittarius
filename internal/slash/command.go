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
}

// MaxDescriptionLen is the fork limit for user-visible command descriptions.
const MaxDescriptionLen = 100
