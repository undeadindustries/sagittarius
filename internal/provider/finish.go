package provider

import (
	"strings"

	"google.golang.org/genai"
)

// promptBlockedPrefix prefixes the FinishReason reported when a provider
// refuses the prompt itself, so no reply was generated at all.
const promptBlockedPrefix = "PROMPT_BLOCKED_"

// FinishNotice turns an abnormal stream finish reason into the line shown to
// the user, or "" for an empty reason.
//
// It exists because a provider can end a reply early (an output-length limit,
// Gemini's RECITATION filter, a safety block) and the stream still closes
// cleanly, so without this the user sees a normal finished reply that simply
// stops mid-sentence and cannot tell a provider stop from a harness bug. The
// hint is deliberately actionable: it names what the user can change, not just
// what happened. Callers decide whether a reason is abnormal; this only formats.
func FinishNotice(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return "Reply cut short by the provider (" + reason + "). " + finishHint(reason)
}

func finishHint(reason string) string {
	lower := strings.ToLower(reason)
	switch {
	case lower == "recitation" || lower == "image_recitation":
		return "It stopped because the output resembled existing text; ask it to paraphrase or describe the code instead of quoting it verbatim."
	case lower == "max_tokens" || lower == "length":
		return "It hit the output limit; ask it to continue."
	case lower == "malformed_function_call" || lower == "unexpected_tool_call":
		return "The model produced an invalid tool call; retry the request."
	case lower == "safety" || lower == "content_filter" || lower == "blocklist" ||
		lower == "prohibited_content" || lower == "spii" || lower == "image_safety" ||
		strings.HasPrefix(lower, strings.ToLower(promptBlockedPrefix)):
		return "A provider content filter blocked it; rephrase the request."
	default:
		return "Ask it to continue or rephrase the request."
	}
}

// geminiFinishIsAbnormal reports whether a Gemini finish reason means the reply
// did not end naturally. Everything except STOP and the unspecified default is
// abnormal, so a reason Google adds later is reported rather than hidden.
func geminiFinishIsAbnormal(reason genai.FinishReason) bool {
	return reason != "" && reason != genai.FinishReasonStop && reason != genai.FinishReasonUnspecified
}

// openAIFinishIsAbnormal reports whether an OpenAI-wire finish_reason means the
// reply was cut short. Only known-bad values count: OpenAI-compatible servers
// differ in vocabulary, and a false alarm on every reply from an unfamiliar
// server is worse than a missed notice.
func openAIFinishIsAbnormal(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "length", "content_filter", "error":
		return true
	}
	return false
}

// abnormalGeminiFinish extracts the abnormal stop reason carried by one Gemini
// response chunk, or "" when the chunk ends normally or carries none. A blocked
// prompt arrives as prompt feedback with no candidates, so it is checked too.
func abnormalGeminiFinish(resp *genai.GenerateContentResponse) string {
	if resp == nil {
		return ""
	}
	if fb := resp.PromptFeedback; fb != nil && fb.BlockReason != "" &&
		fb.BlockReason != genai.BlockedReasonUnspecified {
		return promptBlockedPrefix + string(fb.BlockReason)
	}
	if len(resp.Candidates) > 0 && resp.Candidates[0] != nil &&
		geminiFinishIsAbnormal(resp.Candidates[0].FinishReason) {
		return string(resp.Candidates[0].FinishReason)
	}
	return ""
}
