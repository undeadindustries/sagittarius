package slash

import "strings"

// Suggestion is one completion candidate produced by Registry.Complete.
type Suggestion struct {
	Label       string
	Description string
	Insert      string
	AppendSpace bool
}

// Completion is the result of completing a partial slash input line.
type Completion struct {
	Items []Suggestion
	// ReplaceFrom is the byte offset in the input where the active token begins;
	// accepting a suggestion replaces input[ReplaceFrom:] with the Insert value.
	ReplaceFrom int
}

// Complete returns slash-command, subcommand, and argument completions for a
// partial input line. It is read-only and safe to call on every keystroke; it
// performs no network calls. Non-slash input yields no candidates.
func (r *Registry) Complete(input string, deps Deps) Completion {
	if !strings.HasPrefix(input, "/") {
		return Completion{ReplaceFrom: len(input)}
	}

	hasTrailingSpace := strings.HasSuffix(input, " ")
	rawParts := strings.Fields(input[1:])

	partial := ""
	pathParts := rawParts
	if !hasTrailingSpace && len(rawParts) > 0 {
		partial = rawParts[len(rawParts)-1]
		pathParts = rawParts[:len(rawParts)-1]
	}
	replaceFrom := len(input) - len(partial)

	// Walk the command tree along the resolved path parts.
	level := r.List()
	var leaf *Command
	for _, part := range pathParts {
		found := lookupCommand(level, part)
		if found == nil {
			leaf = nil
			level = nil
			break
		}
		leaf = found
		level = visibleSubcommands(*found)
	}

	// Argument completion: a leaf command with an ArgComplete and no remaining
	// subcommands to disambiguate, once we are positioned past its name.
	pastName := hasTrailingSpace || len(rawParts) > len(pathParts)
	if leaf != nil && leaf.ArgComplete != nil && len(level) == 0 && pastName {
		return argCompletion(leaf, deps, partial, replaceFrom)
	}

	return commandCompletion(level, partial, replaceFrom)
}

// argCompletion builds prefix-filtered suggestions from a command's ArgComplete.
func argCompletion(cmd *Command, deps Deps, partial string, replaceFrom int) Completion {
	values := cmd.ArgComplete(deps, partial)
	lower := strings.ToLower(partial)
	items := make([]Suggestion, 0, len(values))
	for _, v := range values {
		if partial != "" && !strings.HasPrefix(strings.ToLower(v), lower) {
			continue
		}
		items = append(items, Suggestion{Label: v, Insert: v})
	}
	return Completion{Items: items, ReplaceFrom: replaceFrom}
}

// commandCompletion builds prefix-filtered suggestions from visible commands at
// the current tree level.
func commandCompletion(level []Command, partial string, replaceFrom int) Completion {
	lower := strings.ToLower(partial)
	items := make([]Suggestion, 0, len(level))
	for _, cmd := range level {
		if cmd.Hidden {
			continue
		}
		if partial != "" && !strings.HasPrefix(strings.ToLower(cmd.Name), lower) {
			continue
		}
		items = append(items, Suggestion{
			Label:       cmd.Name,
			Description: cmd.Description,
			Insert:      cmd.Name,
			AppendSpace: len(visibleSubcommands(cmd)) > 0 || cmd.ArgComplete != nil,
		})
	}
	return Completion{Items: items, ReplaceFrom: replaceFrom}
}
