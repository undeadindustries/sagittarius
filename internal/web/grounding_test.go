package web

import (
	"testing"

	"google.golang.org/genai"
)

func TestFormatSearchResult(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		text     string
		meta     *genai.GroundingMetadata
		expected string
	}{
		{
			name:  "nil metadata",
			query: "test query",
			text:  "Some text.",
			meta:  nil,
			expected: `Web search results for "test query":

Some text.`,
		},
		{
			name:  "empty chunks",
			query: "test query",
			text:  "Some text.",
			meta:  &genai.GroundingMetadata{},
			expected: `Web search results for "test query":

Some text.`,
		},
		{
			name:  "with sources and insertions",
			query: "apple",
			text:  "Apple is a company. It makes phones.",
			meta: &genai.GroundingMetadata{
				GroundingChunks: []*genai.GroundingChunk{
					{
						Web: &genai.GroundingChunkWeb{
							Title: "Apple Inc.",
							URI:   "https://apple.com",
						},
					},
					{
						Web: &genai.GroundingChunkWeb{
							Title: "iPhone",
							URI:   "https://apple.com/iphone",
						},
					},
				},
				GroundingSupports: []*genai.GroundingSupport{
					{
						Segment: &genai.Segment{
							EndIndex: 19, // After "Apple is a company."
						},
						GroundingChunkIndices: []int32{0},
					},
					{
						Segment: &genai.Segment{
							EndIndex: 36, // After "It makes phones."
						},
						GroundingChunkIndices: []int32{1},
					},
				},
			},
			expected: `Web search results for "apple":

Apple is a company.[1] It makes phones.[2]

Sources:
[1] Apple Inc. (https://apple.com)
[2] iPhone (https://apple.com/iphone)`,
		},
		{
			name:  "multiple supports at same index",
			query: "banana",
			text:  "Bananas are yellow.",
			meta: &genai.GroundingMetadata{
				GroundingChunks: []*genai.GroundingChunk{
					{Web: &genai.GroundingChunkWeb{Title: "Banana Info", URI: "https://example.com/banana"}},
					{Web: &genai.GroundingChunkWeb{Title: "Yellow Things", URI: "https://example.com/yellow"}},
				},
				GroundingSupports: []*genai.GroundingSupport{
					{
						Segment: &genai.Segment{EndIndex: 19},
						GroundingChunkIndices: []int32{0, 1},
					},
				},
			},
			expected: `Web search results for "banana":

Bananas are yellow.[1][2]

Sources:
[1] Banana Info (https://example.com/banana)
[2] Yellow Things (https://example.com/yellow)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatSearchResult(tt.query, tt.text, tt.meta)
			if result != tt.expected {
				t.Errorf("FormatSearchResult() mismatch\nexpected:\n%s\ngot:\n%s", tt.expected, result)
			}
		})
	}
}
