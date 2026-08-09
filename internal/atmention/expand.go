package atmention

import (
	"errors"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

const (
	referenceHeader = "--- Content from referenced files ---"
	referenceFooter = "--- End of referenced files ---"
	skillHeader     = "--- Skills requested for this message ---"
	skillFooter     = "--- End of requested skills ---"
)

// errNoSkills reports a skill mention in a session with no skill manager.
var errNoSkills = errors.New("skills are not available in this session")

// SkillResolver returns model-facing instruction text for a named skill.
// *skills.Manager satisfies it, which keeps this package free of a dependency
// on internal/skills.
type SkillResolver interface {
	Content(name string) (string, error)
}

// Expand turns a user query containing "@path" and "@skill:name" references
// into the message parts sent to the model. The original query text is
// preserved as the first part (scrollback and session history keep showing
// exactly what the user typed); referenced file contents and requested skill
// instructions follow as separate parts wrapped in clear delimiters.
//
// Skill instructions are emitted last so a large file reference in the same
// message cannot bury them right where the model starts generating.
//
// When the query contains no references, Expand returns the query unchanged as
// a single text part. When a reference cannot be resolved (a missing, external,
// directory or binary path, or an unknown skill), Expand returns an error so
// the caller can surface it before starting the turn.
func Expand(ws *tools.Workspace, query string, skills SkillResolver) ([]provider.Part, error) {
	mentions := scanMentions(query)
	if len(mentions) == 0 {
		return []provider.Part{{Text: query}}, nil
	}

	// Skills are resolved against the budget first: the user asked for them by
	// name, so they must not be starved by files that happen to be listed
	// earlier in the same message.
	budget := combinedCap
	skillBlock, err := expandSkills(mentions, skills, &budget)
	if err != nil {
		return nil, err
	}
	fileBlock, err := expandFiles(ws, mentions, &budget)
	if err != nil {
		return nil, err
	}

	parts := []provider.Part{{Text: query}}
	if fileBlock != "" {
		parts = append(parts, provider.Part{Text: fileBlock})
	}
	if skillBlock != "" {
		parts = append(parts, provider.Part{Text: skillBlock})
	}
	return parts, nil
}

// expandSkills renders the instruction block for every "@skill:name" mention,
// drawing each body from the shared injection budget.
func expandSkills(mentions []mention, resolver SkillResolver, budget *int) (string, error) {
	var blocks strings.Builder
	seen := make(map[string]bool, len(mentions))
	included := 0

	for _, m := range mentions {
		if m.kind != kindSkill {
			continue
		}
		key := strings.ToLower(m.name)
		if seen[key] {
			continue
		}
		seen[key] = true

		if resolver == nil {
			return "", fmt.Errorf("@%s%s: %w", skillPrefix, m.name, errNoSkills)
		}
		content, err := resolver.Content(m.name)
		if err != nil {
			return "", fmt.Errorf("@%s%s: %w", skillPrefix, m.name, err)
		}
		content, truncated := capString(content, *budget)
		*budget -= len(content)
		switch {
		case content == "":
			// The budget is spent (many large skills in one message). Say so
			// rather than emitting a stray closing tag with no content.
			content = fmt.Sprintf("(skill %q omitted: this message's injection budget is exhausted)", m.name)
		case truncated:
			// Re-close the tag the cap cut off so the block stays well formed.
			content += "\n... (truncated)\n</activated_skill>"
		}
		included++

		if included == 1 {
			blocks.WriteString("\n")
			blocks.WriteString(skillHeader)
			blocks.WriteString("\n")
		}
		blocks.WriteString("\n")
		blocks.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			blocks.WriteString("\n")
		}
		fmt.Fprintf(&blocks, "The user explicitly requested this skill for this message and it is already loaded. Do not call activate_skill for %q.\n", m.name)
	}

	if included == 0 {
		return "", nil
	}
	blocks.WriteString(skillFooter)
	blocks.WriteString("\n")
	return blocks.String(), nil
}

// expandFiles renders the content block for every "@path" mention.
func expandFiles(ws *tools.Workspace, mentions []mention, budget *int) (string, error) {
	if ws == nil {
		return "", nil
	}

	var blocks strings.Builder
	seen := make(map[string]bool, len(mentions))
	included := 0

	for _, m := range mentions {
		if m.kind != kindPath {
			continue
		}
		if seen[m.name] {
			continue
		}
		seen[m.name] = true

		ref, err := resolveMention(ws, m.name)
		if err != nil {
			return "", fmt.Errorf("@%s: %w", m.name, err)
		}
		content, truncated, err := readCapped(ref.abs, *budget)
		if err != nil {
			return "", fmt.Errorf("@%s: %w", m.name, err)
		}
		*budget -= len(content)
		included++

		if included == 1 {
			blocks.WriteString("\n")
			blocks.WriteString(referenceHeader)
			blocks.WriteString("\n")
		}
		fmt.Fprintf(&blocks, "\nFile: @%s (%s)\n", ref.display, ref.abs)
		blocks.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			blocks.WriteString("\n")
		}
		if truncated {
			blocks.WriteString("... (truncated)\n")
		}
	}

	if included == 0 {
		return "", nil
	}
	blocks.WriteString(referenceFooter)
	blocks.WriteString("\n")
	return blocks.String(), nil
}
