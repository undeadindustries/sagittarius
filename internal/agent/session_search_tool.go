package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// searchSessionTool lets the model recover an exact detail from earlier in the
// conversation after compression has summarized it away. The session JSONL is
// the full append-only transcript, so it still holds text the live history no
// longer does.
//
// Scope is deliberately the live session only. Cross-session search overlaps
// MemPalace's job and would need a path-confinement story of its own.
type searchSessionTool struct {
	runner *Runner
}

func newSearchSessionTool(r *Runner) tools.Tool {
	return &searchSessionTool{runner: r}
}

func (t *searchSessionTool) Name() string { return tools.SearchSessionToolName }

func (t *searchSessionTool) Description() string {
	return fmt.Sprintf(
		"Search the full transcript of this conversation for a literal, case-insensitive substring. "+
			"Use it to recover an exact earlier detail — a path, port, id, command, or decision — "+
			"that compression has since summarized away, instead of guessing or asking the user to repeat it. "+
			"Searches your own messages and the user's, including tool-call arguments and results. "+
			"Returns the most recent matches first with surrounding context. "+
			"Read-only. Default %d results, maximum %d.",
		session.SearchDefaultMaxResults, session.SearchMaxResults,
	)
}

func (t *searchSessionTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.SearchSessionParamQuery: map[string]any{
					"type":        "string",
					"description": "Literal text to find. Not a regular expression. Case-insensitive.",
				},
				tools.SearchSessionParamRole: map[string]any{
					"type":        "string",
					"enum":        []string{string(session.SearchRoleAny), string(session.SearchRoleUser), string(session.SearchRoleModel)},
					"description": "Restrict to the user's messages or your own. Defaults to any.",
				},
				tools.SearchSessionParamMaxResults: map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum matches to return (default %d, capped at %d).", session.SearchDefaultMaxResults, session.SearchMaxResults),
				},
			},
			"required": []string{tools.SearchSessionParamQuery},
		},
	}
}

func (t *searchSessionTool) RequiresConfirmation() bool { return false }

func (t *searchSessionTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	query, err := stringArg(args, tools.SearchSessionParamQuery)
	if err != nil {
		return nil, err
	}
	role, err := parseSearchRole(args[tools.SearchSessionParamRole])
	if err != nil {
		return nil, err
	}

	path := t.runner.SessionFilePath()
	res, err := session.Search(path, session.SearchOptions{
		Query:      query,
		Role:       role,
		MaxResults: optionalIntArg(args[tools.SearchSessionParamMaxResults]),
	})
	if err != nil {
		if errors.Is(err, session.ErrSearchEmptyQuery) {
			return nil, err
		}
		return nil, fmt.Errorf("search session: %w", err)
	}

	return map[string]any{
		"query":     query,
		"count":     res.Total,
		"returned":  len(res.Matches),
		"truncated": res.Total > len(res.Matches),
		"matches":   renderMatches(res),
	}, nil
}

// parseSearchRole accepts the three documented values and rejects anything else
// rather than silently widening to "any", which would make a typo look like a
// filter that found nothing.
func parseSearchRole(v any) (session.SearchRole, error) {
	if v == nil {
		return session.SearchRoleAny, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("parameter %q must be a string", tools.SearchSessionParamRole)
	}
	switch role := session.SearchRole(strings.ToLower(strings.TrimSpace(s))); role {
	case "", session.SearchRoleAny, session.SearchRoleUser, session.SearchRoleModel:
		if role == "" {
			return session.SearchRoleAny, nil
		}
		return role, nil
	default:
		return "", fmt.Errorf("parameter %q must be one of any, user, model; got %q",
			tools.SearchSessionParamRole, s)
	}
}

// optionalIntArg reads a count that may arrive as a JSON number (float64) from
// a wire decode or as an int from a local call. A non-numeric or absent value
// yields 0, which Search treats as "use the default".
func optionalIntArg(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// renderMatches formats the hits as plain text. A model reads this far more
// reliably than nested JSON, and it keeps the tool card readable.
func renderMatches(res session.SearchResult) string {
	if len(res.Matches) == 0 {
		return ""
	}
	var b strings.Builder
	if res.SkippedLines > 0 {
		fmt.Fprintf(&b, "Note: %d oversized transcript line(s) could not be searched.\n\n", res.SkippedLines)
	}
	for i, m := range res.Matches {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "[turn %d | %s", m.TurnIndex, m.Role)
		if m.Timestamp != "" {
			fmt.Fprintf(&b, " | %s", m.Timestamp)
		}
		b.WriteString("]\n")
		b.WriteString(m.Excerpt)
	}
	return b.String()
}
