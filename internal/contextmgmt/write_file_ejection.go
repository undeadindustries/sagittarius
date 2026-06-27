package contextmgmt

// ejection bounds ported from the fork.
const (
	minEjectionAgeTurns = 1
	maxEjectionAgeTurns = 10
)

// WriteFileEjectionOptions configures EjectStaleWriteFileContent.
type WriteFileEjectionOptions struct {
	// WriteFileToolName identifies a write_file functionCall (e.g. "write_file").
	WriteFileToolName string
	// ExemptTools are never ejected, even if they carry a content arg.
	ExemptTools map[string]bool
	// ProtectLatestTurn leaves the most recent turn untouched when true.
	ProtectLatestTurn bool
	// MinAgeTurns is the minimum age (turns from the end) before a call is
	// eligible for ejection. Clamped to [1,10].
	MinAgeTurns int
	// MinTokensPerCall skips content payloads below this estimated token count.
	MinTokensPerCall int
	// Estimate overrides the default token estimator (test seam).
	Estimate EstimateFn
}

// WriteFileEjectionResult reports the outcome of an ejection pass.
type WriteFileEjectionResult struct {
	// NewHistory has stale write_file content removed (the content arg dropped).
	NewHistory []Message
	// EjectedCount is the number of calls ejected this pass.
	EjectedCount int
	// TokensSaved is the estimated tokens reclaimed across all ejected calls.
	TokensSaved int
}

// EjectStaleWriteFileContent drops the content arg of stale write_file
// functionCall parts, preserving file_path so the model can re-read the file. It
// never touches the leading entries, the protected latest turn, calls newer than
// MinAgeTurns, exempt tools, or already-ejected calls (whose content arg is
// already gone). The input history is not mutated. The content is removed rather
// than replaced with a marker because models copy a marker left in the content
// position into their next write_file call, triggering a rejection loop.
func EjectStaleWriteFileContent(history []Message, opts WriteFileEjectionOptions) WriteFileEjectionResult {
	if len(history) <= preserveLeadingEntries {
		return WriteFileEjectionResult{NewHistory: cloneHistory(history)}
	}

	estimate := opts.Estimate
	if estimate == nil {
		estimate = EstimateTokens
	}
	minAge := clampInt(opts.MinAgeTurns, minEjectionAgeTurns, maxEjectionAgeTurns)
	minTokens := maxInt(0, opts.MinTokensPerCall)

	lastTurnIdx := len(history) - 1
	protectedFromIdx := lastTurnIdx + 1
	if opts.ProtectLatestTurn {
		protectedFromIdx = maxInt(0, lastTurnIdx-(minAge-1))
	}

	result := WriteFileEjectionResult{NewHistory: make([]Message, len(history))}
	for idx := range history {
		content := history[idx]
		if idx < preserveLeadingEntries || idx >= protectedFromIdx || len(content.Parts) == 0 {
			result.NewHistory[idx] = content
			continue
		}
		newParts, ejected, saved := ejectParts(content.Parts, opts, estimate, minTokens)
		if ejected == 0 {
			result.NewHistory[idx] = content
			continue
		}
		result.NewHistory[idx] = Message{Role: content.Role, Parts: newParts}
		result.EjectedCount += ejected
		result.TokensSaved += saved
	}
	return result
}

func ejectParts(parts []Part, opts WriteFileEjectionOptions, estimate EstimateFn, minTokens int) ([]Part, int, int) {
	ejected := 0
	saved := 0
	newParts := make([]Part, len(parts))
	copy(newParts, parts)

	for i := range parts {
		fc := parts[i].FunctionCall
		if fc == nil || fc.Name != opts.WriteFileToolName || opts.ExemptTools[fc.Name] {
			continue
		}
		content, ok := stringArg(fc.Args, WriteFileParamContent)
		if !ok {
			// Already ejected (content arg dropped) or never present.
			continue
		}
		contentTokens := estimate([]Part{{Text: content}})
		if contentTokens < minTokens {
			continue
		}

		// Drop the content arg entirely rather than substituting a marker
		// string. A structured marker left in the content position is treated by
		// many models as a template and copied verbatim into the next write_file
		// call (rejected as "looks like a context ejection marker"), causing a
		// retry loop. With no content value present there is nothing to copy; the
		// paired tool result still records that the write succeeded, and the file
		// on disk is intact (the model can read_file to view it).
		newArgs := copyArgs(fc.Args)
		delete(newArgs, WriteFileParamContent)
		newParts[i] = Part{
			FunctionCall:     &ToolCall{ID: fc.ID, Name: fc.Name, Args: newArgs},
			ThoughtSignature: parts[i].ThoughtSignature,
		}
		ejected++
		saved += contentTokens
	}
	return newParts, ejected, saved
}

// WriteFileParamContent is the args key holding the write_file payload, mirrored
// from the tools package wire name to keep this file dependency-light.
const WriteFileParamContent = "content"

func stringArg(args map[string]any, key string) (string, bool) {
	if args == nil {
		return "", false
	}
	v, ok := args[key].(string)
	return v, ok
}

func copyArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
