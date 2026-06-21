package contextmgmt

import (
	"encoding/json"
	"math"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Token estimation is a deterministic heuristic ported from the fork
// (utils/tokenCalculation.ts estimateTokenCountSync). It deliberately avoids a
// real tokenizer dependency: the budget math only needs a stable, conservative
// approximation, and pulling in a heavyweight BPE tokenizer would violate the
// stdlib-first guideline. The approximation is:
//
//   - ASCII text: defaultCharsPerToken characters per token (~4 chars/token).
//   - Non-ASCII (CJK, emoji, …): nonASCIITokensPerChar tokens per character,
//     conservatively high to avoid underestimating multi-byte scripts.
//   - functionResponse: name length plus JSON-encoded response length / cpt.
//   - Any other part (e.g. functionCall): JSON-encoded length / cpt.
const (
	// defaultCharsPerToken is the divisor applied to ASCII character counts.
	defaultCharsPerToken = 4
	// nonASCIITokensPerChar is the per-character cost for non-ASCII runes.
	nonASCIITokensPerChar = 1.5
	// maxCharsForFullHeuristic bounds the per-character scan; above it a faster
	// length/cpt approximation is used to avoid pathological cost on huge blobs.
	maxCharsForFullHeuristic = 100_000
	// maxASCII is the highest code point treated as single-byte/ASCII.
	maxASCII = 127
)

// EstimateFn estimates the token count of a slice of parts. It matches the
// shape the fork mocks in its unit tests so masking/ejection can be tested with
// deterministic token values.
type EstimateFn func(parts []provider.Part) int

// EstimateTokens returns a heuristic token count for the supplied parts.
// It is deterministic and side-effect free. See the package constants for the
// approximation used and its rationale.
func EstimateTokens(parts []provider.Part) int {
	total := 0.0
	for i := range parts {
		total += estimatePartTokens(parts[i])
	}
	return int(math.Floor(total))
}

func estimatePartTokens(part provider.Part) float64 {
	switch {
	case part.Text != "":
		return estimateTextTokens(part.Text)
	case part.FunctionResponse != nil:
		return estimateFunctionResponseTokens(part.FunctionResponse)
	default:
		return jsonLen(part) / defaultCharsPerToken
	}
}

func estimateTextTokens(text string) float64 {
	if len(text) > maxCharsForFullHeuristic {
		return float64(len(text)) / defaultCharsPerToken
	}
	tokens := 0.0
	asciiPerChar := 1.0 / defaultCharsPerToken
	for _, r := range text {
		if r <= maxASCII {
			tokens += asciiPerChar
		} else {
			tokens += nonASCIITokensPerChar
		}
	}
	return tokens
}

func estimateFunctionResponseTokens(fr *provider.FunctionResponse) float64 {
	tokens := float64(len(fr.Name)) / defaultCharsPerToken
	if len(fr.Response) > 0 {
		tokens += jsonLen(fr.Response) / defaultCharsPerToken
	}
	return tokens
}

// jsonLen returns the length of the compact JSON encoding of v, or 0 when v is
// not encodable. The encoder disables HTML escaping to mirror JS JSON.stringify
// byte counts for typical payloads.
func jsonLen(v any) float64 {
	b, err := marshalCompact(v)
	if err != nil {
		return 0
	}
	return float64(len(b))
}

func marshalCompact(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}
