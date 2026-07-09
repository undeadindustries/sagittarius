package slash

import (
	"fmt"
	"sort"
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
	b.WriteString("\nInput / Tools:\n\n")
	fmt.Fprintf(&b, "  %-28s %s\n", "@path/to/file",
		"reference a file; its contents are sent to the model (tab to autocomplete)")
	fmt.Fprintf(&b, "  %-28s %s\n", "Web Tools",
		"google_web_search and web_fetch are available (Gemini API key required)")

	b.WriteString("\nKeyboard shortcuts:\n\n")
	fmt.Fprintf(&b, "  %-28s %s\n", "Alt+1..4 (or ⌥+1..4)", "Switch mode (agent/plan/ask/debug)")
	fmt.Fprintf(&b, "  %-28s %s\n", "Ctrl+Shift+M", "Cycle mode (agent → plan → ask → debug)")
	fmt.Fprintf(&b, "  %-28s %s\n", "Ctrl+/", "Cycle active models forward")
	fmt.Fprintf(&b, "  %-28s %s\n", "Ctrl+Shift+P", "Cycle active models backward")
	fmt.Fprintf(&b, "  %-28s %s\n", "Alt+T (or ⌥+T)", "Cycle color theme (default ↔ greyscale)")
	fmt.Fprintf(&b, "  %-28s %s\n", "Alt+M (or ⌥+M)", "Toggle mouse-wheel scrolling")
	fmt.Fprintf(&b, "  %-28s %s\n", "Ctrl+T", "Toggle thinking box")
	fmt.Fprintf(&b, "  %-28s %s\n", "Ctrl+B", "Open background process viewer")
	fmt.Fprintf(&b, "  %-28s %s\n", "PgUp/PgDn, Shift+Up/Down", "Scroll conversation")
	fmt.Fprintf(&b, "  %-28s %s\n", "Esc / Ctrl+C", "Cancel turn / Quit")

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
		aboutCommand(),
		quitCommand(),
		resumeCommand(),
		chatCommand(),
		compressCommand(),
		copyCommand(),
		statsCommand(),
		initCommand(),
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
		toolsCommand(),
		agentsCommand(),
		reasoningCommand(),
		modeCommand(),
		themeCommand(),
		mouseCommand(),
		settingsCommand(),
		goalCommand(),
		grillCommand(),
	}
	r.sortCommandTree(r.commands)
}

func (r *Registry) sortCommandTree(cmds []Command) {
	sort.Slice(cmds, func(i, j int) bool {
		return strings.ToLower(cmds[i].Name) < strings.ToLower(cmds[j].Name)
	})
	for i := range cmds {
		// Do not sort the reasoning levels (minimal -> high).
		if cmds[i].Name == "reasoning" {
			continue
		}
		if len(cmds[i].SubCommands) > 0 {
			r.sortCommandTree(cmds[i].SubCommands)
		}
	}
}
