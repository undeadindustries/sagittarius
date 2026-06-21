package contextmgmt

import "github.com/undeadindustries/sagittarius/internal/provider"

// The context defenses operate on the provider domain types directly. These
// aliases keep signatures readable without inventing a parallel type system —
// Message, Part, etc. are exactly the provider types (see AD-011/AD-015).
type (
	// Message is one turn in the conversation history.
	Message = provider.Message
	// Part is a single content fragment within a Message.
	Part = provider.Part
	// ToolCall is a model-initiated function invocation.
	ToolCall = provider.ToolCall
	// FunctionResponse carries a tool result back to the model.
	FunctionResponse = provider.FunctionResponse
)

// Role constants re-exported for local readability.
const (
	RoleUser  = provider.RoleUser
	RoleModel = provider.RoleModel
)

// cloneHistory returns a shallow copy of the history slice. The element structs
// are not deep-copied; callers that mutate parts must replace them rather than
// edit in place (matching the pure-function contract of these helpers).
func cloneHistory(history []Message) []Message {
	if history == nil {
		return nil
	}
	out := make([]Message, len(history))
	copy(out, history)
	return out
}

// concatHistory joins two history slices into a fresh slice.
func concatHistory(a, b []Message) []Message {
	out := make([]Message, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out
}
