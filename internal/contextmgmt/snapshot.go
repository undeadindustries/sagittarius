package contextmgmt

import "strings"

// Summarizer output cleanup.
//
// The compression prompt asks the summarizer to reason in a private
// <scratchpad> before emitting the <state_snapshot>. "Private" was aspirational:
// the whole response used to be stored verbatim, so a measured session carried
// 9,323 characters of scratchpad inside a 33,188-character "compressed" summary
// — 28% of the artifact whose entire job is to save tokens. Because a stored
// snapshot is replayed into the next summarization via
// anchoredSnapshotInstruction, that waste compounds across compactions.
//
// Extraction also hardens the empty-summary guard in Compress. A thinking model
// that spends its output budget reasoning and never reaches the snapshot used to
// produce a non-empty response, so the guard passed and history was replaced by
// pure reasoning. Extracting first turns that case into an empty summary, which
// fails closed and leaves the conversation intact.
const (
	snapshotOpenTag  = "<state_snapshot>"
	snapshotCloseTag = "</state_snapshot>"
)

// reasoningTags are the wrappers models use for private reasoning: our own
// prompted scratchpad plus the inline forms thinking models emit when a native
// reasoning channel is unavailable. Matched case-sensitively, since folding
// would have to lowercase the whole summary and Unicode special casing can
// change byte length, desynchronizing the offsets used to slice it.
var reasoningTags = []string{"scratchpad", "think", "thinking", "reasoning"}

// extractSnapshot reduces a raw summarizer response to the text worth storing.
//
// A response containing a <state_snapshot> yields that element alone. Otherwise
// the response is kept minus any reasoning wrappers, because a model that
// ignores the requested envelope can still return a usable prose summary and
// discarding it would cost the whole compression. A response that is nothing but
// reasoning yields the empty string, which Compress treats as a failure.
func extractSnapshot(raw string) string {
	start := strings.Index(raw, snapshotOpenTag)
	if start < 0 {
		return strings.TrimSpace(stripReasoningBlocks(raw))
	}
	if end := strings.LastIndex(raw, snapshotCloseTag); end > start {
		return strings.TrimSpace(raw[start : end+len(snapshotCloseTag)])
	}
	// Opening tag with no closer: the response was cut off mid-snapshot. The
	// leading sections (goal, key knowledge) are the densest part, so a partial
	// snapshot beats discarding the compression entirely.
	return strings.TrimSpace(raw[start:])
}

// stripReasoningBlocks removes reasoning wrappers and their contents.
func stripReasoningBlocks(raw string) string {
	for _, tag := range reasoningTags {
		raw = stripTagBlock(raw, "<"+tag+">", "</"+tag+">")
	}
	return raw
}

// stripTagBlock removes every openTag..closeTag pair, plus the unpaired remnants
// a truncated or malformed response leaves behind: a stray closer means the
// model omitted its opening tag, and a stray opener means no summary followed.
func stripTagBlock(s, openTag, closeTag string) string {
	for {
		o, c := strings.Index(s, openTag), strings.Index(s, closeTag)
		switch {
		case o < 0 && c < 0:
			return s
		case c >= 0 && (o < 0 || c < o):
			s = s[c+len(closeTag):]
		case c < 0:
			return s[:o]
		default:
			s = s[:o] + s[c+len(closeTag):]
		}
	}
}
