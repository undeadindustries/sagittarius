package provider

import "github.com/undeadindustries/sagittarius/internal/config"

// FilterHistoryForTarget returns a view of history safe for the target
// (provider, model, wire) without mutating the underlying slice.
//
// Rules (OpenCode / DeepSeek TUI pattern):
//   - RoleUser messages pass through untouched.
//   - Same provider (and compatible wire): keep ThoughtSignature,
//     RedactedThinking, and Reasoning intact. Same-provider model changes
//     preserve signatures (Anthropic drop_block handles any binding mismatch).
//   - Different provider, different wire, or unknown provenance (legacy JSONL
//     without Round metadata): drop ThoughtSignature, RedactedThinking, and
//     Reasoning on the outbound view so foreign signatures/reasoning never
//     reach an incompatible provider.
func FilterHistoryForTarget(
	history []Message,
	targetProvider, targetModel string,
	targetWire config.WireFormat,
) []Message {
	_ = targetModel
	if len(history) == 0 {
		return history
	}
	normTarget := config.NormalizeProviderID(targetProvider)
	if normTarget == "" && targetWire == "" {
		return history
	}

	var out []Message
	for i := range history {
		msg := history[i]
		if msg.Role != RoleModel || messageProvenanceMatches(msg, normTarget, targetWire) {
			if out != nil {
				out = append(out, msg)
			}
			continue
		}

		filtered, changed := stripProviderBoundParts(msg)
		if !changed {
			if out != nil {
				out = append(out, msg)
			}
			continue
		}
		if out == nil {
			out = make([]Message, i, len(history))
			copy(out, history[:i])
		}
		if len(filtered.Parts) > 0 {
			out = append(out, filtered)
		}
	}
	if out == nil {
		return history
	}
	return out
}

func messageProvenanceMatches(msg Message, normTarget string, targetWire config.WireFormat) bool {
	normMsg := config.NormalizeProviderID(msg.Provider)
	if normMsg == "" {
		// Unknown provenance (e.g. legacy JSONL without Round metadata):
		// drop provider-bound data rather than risking a 400.
		return false
	}
	if normTarget != "" && normMsg != normTarget {
		return false
	}
	if msg.Wire != "" && targetWire != "" && msg.Wire != targetWire {
		return false
	}
	return true
}

func stripProviderBoundParts(msg Message) (Message, bool) {
	changed := false
	parts := make([]Part, 0, len(msg.Parts))
	for _, p := range msg.Parts {
		hasBound := len(p.ThoughtSignature) > 0 || p.RedactedThinking != "" || p.Reasoning != ""
		if !hasBound {
			parts = append(parts, p)
			continue
		}
		changed = true
		p.ThoughtSignature = nil
		p.RedactedThinking = ""
		p.Reasoning = ""
		if p.Text == "" && p.FunctionCall == nil && p.FunctionResponse == nil {
			continue
		}
		parts = append(parts, p)
	}
	if !changed {
		return msg, false
	}
	return Message{
		Role:     msg.Role,
		Parts:    parts,
		Provider: msg.Provider,
		Model:    msg.Model,
		Wire:     msg.Wire,
	}, true
}
