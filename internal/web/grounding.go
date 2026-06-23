package web

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/genai"
)

// FormatSearchResult formats a search response text with inline citation markers
// [1][2] and a Sources list appended at the end, using the grounding metadata.
// It inserts markers using UTF-8 byte indices as returned by the Gemini API.
func FormatSearchResult(query, text string, meta *genai.GroundingMetadata) string {
	if meta == nil || len(meta.GroundingChunks) == 0 {
		return fmt.Sprintf("Web search results for %q:\n\n%s", query, text)
	}

	sourceListFormatted := make([]string, 0, len(meta.GroundingChunks))
	for i, chunk := range meta.GroundingChunks {
		title := "Untitled"
		uri := "No URI"
		if chunk.Web != nil {
			if chunk.Web.Title != "" {
				title = chunk.Web.Title
			}
			if chunk.Web.URI != "" {
				uri = chunk.Web.URI
			}
		}
		sourceListFormatted = append(sourceListFormatted, fmt.Sprintf("[%d] %s (%s)", i+1, title, uri))
	}

	type insertion struct {
		index  int
		marker string
	}

	var insertions []insertion
	if len(meta.GroundingSupports) > 0 {
		for _, support := range meta.GroundingSupports {
			if support.Segment != nil && len(support.GroundingChunkIndices) > 0 {
				var markerBuilder strings.Builder
				for _, idx := range support.GroundingChunkIndices {
					// chunk index is 0-based in metadata, we display as 1-based
					markerBuilder.WriteString(fmt.Sprintf("[%d]", idx+1))
				}
				insertions = append(insertions, insertion{
					index:  int(support.Segment.EndIndex),
					marker: markerBuilder.String(),
				})
			}
		}

		// Sort insertions by index in descending order to avoid shifting subsequent indices
		sort.SliceStable(insertions, func(i, j int) bool {
			return insertions[i].index > insertions[j].index
		})

		// Combine insertions that happen at the same index so we don't reverse their
		// order incorrectly, though sort.SliceStable preserves relative order.
		// Wait, if they are at the same index, we just append them.
		
		textBytes := []byte(text)
		var parts [][]byte
		lastIndex := len(textBytes)

		for _, ins := range insertions {
			pos := ins.index
			if pos > lastIndex {
				pos = lastIndex
			}
			if pos < 0 {
				pos = 0
			}

			// Prepend the segment after the marker, then the marker
			parts = append([][]byte{textBytes[pos:lastIndex]}, parts...)
			parts = append([][]byte{[]byte(ins.marker)}, parts...)
			lastIndex = pos
		}
		// Prepend any remaining text at the beginning
		if lastIndex > 0 {
			parts = append([][]byte{textBytes[:lastIndex]}, parts...)
		}

		text = string(bytes.Join(parts, nil))
	}

	if len(sourceListFormatted) > 0 {
		text += "\n\nSources:\n" + strings.Join(sourceListFormatted, "\n")
	}

	return fmt.Sprintf("Web search results for %q:\n\n%s", query, text)
}
