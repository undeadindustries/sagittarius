package slash

import (
	"strings"
)

// Parsed holds the resolved command path and trailing arguments.
type Parsed struct {
	Command *Command
	Args    string
	Path    []string
}

// ParseSlashCommand parses raw user input against the registry.
// Returns nil Command when input is not a slash command or is unknown.
func ParseSlashCommand(query string, registry *Registry) Parsed {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") {
		return Parsed{}
	}

	parts := strings.Fields(strings.TrimSpace(trimmed[1:]))
	if len(parts) == 0 {
		return Parsed{}
	}

	current := registry.List()
	var matched *Command
	var parent *Command
	path := make([]string, 0, len(parts))
	pathIndex := 0

	for _, part := range parts {
		found := lookupCommand(current, part)
		if found == nil {
			break
		}
		parent = matched
		matched = found
		path = append(path, found.Name)
		pathIndex++
		if len(found.SubCommands) > 0 {
			current = found.SubCommands
			continue
		}
		break
	}

	if matched == nil {
		return Parsed{}
	}

	args := strings.Join(parts[pathIndex:], " ")

	// Backtrack when a leaf subcommand was matched but args remain and the
	// parent has a handler (fork parseSlashCommand takesArgs=false backtrack).
	if matched.Handler == nil && parent != nil && parent.Handler != nil && args != "" {
		return Parsed{
			Command: parent,
			Args:    strings.Join(parts[pathIndex-1:], " "),
			Path:    path[:len(path)-1],
		}
	}

	return Parsed{
		Command: matched,
		Args:    args,
		Path:    path,
	}
}

func lookupCommand(commands []Command, name string) *Command {
	lower := strings.ToLower(name)
	for i := range commands {
		cmd := &commands[i]
		if strings.EqualFold(cmd.Name, lower) {
			return cmd
		}
	}
	return nil
}

// IsSlashInput reports whether line should be routed to the slash processor.
func IsSlashInput(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "/")
}
