package slash

import (
	"fmt"
	"strings"
)

// Registry holds the built-in slash command tree.
type Registry struct {
	commands []Command
}

// NewRegistry returns a registry with all built-in slash commands registered.
func NewRegistry() *Registry {
	r := &Registry{}
	r.registerBuiltins()
	return r
}

// List returns top-level commands (non-hidden only).
func (r *Registry) List() []Command {
	out := make([]Command, 0, len(r.commands))
	for _, cmd := range r.commands {
		if cmd.Hidden {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// Lookup resolves a slash path like ["provider", "list"] to a command.
func (r *Registry) Lookup(path []string) *Command {
	if len(path) == 0 {
		return nil
	}
	current := r.List()
	var matched *Command
	for _, part := range path {
		found := lookupCommand(current, part)
		if found == nil {
			return matched
		}
		matched = found
		if len(found.SubCommands) == 0 {
			return matched
		}
		current = found.SubCommands
	}
	return matched
}

// RenderHelp returns a text table of commands and subcommands for /help.
func (r *Registry) RenderHelp() string {
	var b strings.Builder
	b.WriteString("Slash commands:\n\n")
	for _, cmd := range r.List() {
		writeCommandHelp(&b, cmd, "")
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeCommandHelp(b *strings.Builder, cmd Command, prefix string) {
	var name string
	if prefix == "" {
		name = "/" + cmd.Name
	} else {
		name = prefix + " " + cmd.Name
	}
	fmt.Fprintf(b, "  %-28s %s\n", name, cmd.Description)
	for _, sub := range visibleSubcommands(cmd) {
		writeCommandHelp(b, sub, name)
	}
}

func visibleSubcommands(cmd Command) []Command {
	out := make([]Command, 0, len(cmd.SubCommands))
	for _, sub := range cmd.SubCommands {
		if sub.Hidden {
			continue
		}
		out = append(out, sub)
	}
	return out
}

func (r *Registry) registerBuiltins() {
	r.commands = []Command{
		helpCommand(),
		quitCommand(),
		resumeCommand(),
		clearCommand(),
		diffCommand(),
		undoCommand(),
		providerCommand(),
		modelCommand(),
		modelsCommand(),
		systemPromptCommand(),
		modesCommand(),
		memoryCommand(),
		skillsCommand(),
		mcpCommand(),
		agentsCommand(),
		reasoningCommand(),
		modeCommand(),
	}
}
