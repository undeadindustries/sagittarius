package agent

import (
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestLastAssistantText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		history []provider.Message
		want    string
	}{
		{
			name:    "empty history",
			history: nil,
			want:    "",
		},
		{
			name: "two text parts joined",
			history: []provider.Message{
				{Role: provider.RoleModel, Parts: []provider.Part{
					{Text: "first"},
					{Text: "second"},
				}},
			},
			want: "first\nsecond",
		},
		{
			name: "trailing user message after model",
			history: []provider.Message{
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "model reply"}}},
				{Role: provider.RoleUser, Parts: []provider.Part{{Text: "follow-up"}}},
			},
			want: "model reply",
		},
		{
			name: "skips tool-only model turn",
			history: []provider.Message{
				{Role: provider.RoleModel, Parts: []provider.Part{{Text: "earlier text"}}},
				{Role: provider.RoleModel, Parts: []provider.Part{
					{FunctionCall: &provider.ToolCall{ID: "1", Name: "read_file"}},
				}},
			},
			want: "earlier text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := lastAssistantText(tt.history); got != tt.want {
				t.Fatalf("lastAssistantText() = %q, want %q", got, tt.want)
			}
		})
	}
}
