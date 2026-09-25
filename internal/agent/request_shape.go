package agent

import "github.com/undeadindustries/sagittarius/internal/provider"

// trimTrailingModelText removes assistant text messages from the end of a
// request so it ends on a user message, a tool result, or an assistant tool
// call awaiting its result. Anthropic (Opus 4.6 and later) rejects a trailing
// assistant message as unsupported prefill, and Gemini requires multi-turn
// requests to end on the user role. OpenAI-compatible and local servers accept
// one but continue it instead of answering, and Sagittarius never prefills, so
// a trailing assistant text is always a history bug. A lone first message is
// kept because an empty request fails less clearly than the provider's own
// error. It returns the trimmed slice (sharing the input's backing array) and
// how many messages were removed.
func trimTrailingModelText(messages []provider.Message) ([]provider.Message, int) {
	end := len(messages)
	for end > 1 && messages[end-1].Role == provider.RoleModel && !messageHasToolCalls(messages[end-1]) {
		end--
	}
	return messages[:end], len(messages) - end
}
