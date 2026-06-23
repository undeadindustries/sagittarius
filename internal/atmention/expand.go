package atmention

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

const (
	referenceHeader = "--- Content from referenced files ---"
	referenceFooter = "--- End of referenced files ---"
)

// Expand turns a user query containing "@path" references into the message parts
// sent to the model. The original query text is preserved as the first part
// (scrollback and session history keep showing exactly what the user typed); a
// second part carries the referenced file contents wrapped in clear delimiters.
//
// When the query contains no "@" references, Expand returns the query unchanged
// as a single text part. When a referenced path cannot be resolved (missing,
// outside the workspace, a directory, or binary), Expand returns an error so the
// caller can surface it before starting the turn.
func Expand(ws *tools.Workspace, query string) ([]provider.Part, error) {
	paths := scanMentions(query)
	if len(paths) == 0 || ws == nil {
		return []provider.Part{{Text: query}}, nil
	}

	var blocks strings.Builder
	blocks.WriteString("\n")
	blocks.WriteString(referenceHeader)
	blocks.WriteString("\n")

	seen := make(map[string]bool, len(paths))
	budget := combinedCap
	included := 0
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true

		ref, err := resolveMention(ws, p)
		if err != nil {
			return nil, fmt.Errorf("@%s: %w", p, err)
		}
		content, truncated, err := readCapped(ref.abs, budget)
		if err != nil {
			return nil, fmt.Errorf("@%s: %w", p, err)
		}
		budget -= len(content)
		included++

		fmt.Fprintf(&blocks, "\nFile: @%s (%s)\n", ref.display, ref.abs)
		blocks.WriteString(content)
		if !strings.HasSuffix(content, "\n") {
			blocks.WriteString("\n")
		}
		if truncated {
			blocks.WriteString("... (truncated)\n")
		}
	}

	blocks.WriteString(referenceFooter)
	blocks.WriteString("\n")

	if included == 0 {
		return []provider.Part{{Text: query}}, nil
	}
	return []provider.Part{{Text: query}, {Text: blocks.String()}}, nil
}
