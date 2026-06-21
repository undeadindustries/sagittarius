package contextmgmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Compression constants ported from chatCompressionService.ts.
const (
	// DefaultCompressionTokenThreshold is the fallback fraction of the context
	// window above which history is compressed.
	DefaultCompressionTokenThreshold = 0.5
	// DefaultLocalCompressionThreshold is the local-mode default (compress earlier).
	DefaultLocalCompressionThreshold = 0.4
	// DefaultLocalPreserveFraction is the fraction of recent history kept raw.
	DefaultLocalPreserveFraction = 0.2
	// compressionFunctionResponseTokenBudget caps preserved tool-response tokens.
	compressionFunctionResponseTokenBudget = 50_000
	// DefaultTruncateToolOutputThreshold is the char budget for budget truncation.
	DefaultTruncateToolOutputThreshold = 40_000
	// truncateHeadFraction is the head share of the truncate threshold.
	truncateHeadFraction = 0.2
)

// CompressionStatus mirrors the fork CompressionStatus enum.
type CompressionStatus int

const (
	// CompressionNoOp means nothing was compressed.
	CompressionNoOp CompressionStatus = iota
	// Compressed means history was summarized successfully.
	Compressed
	// ContentTruncated means only budget truncation was applied.
	ContentTruncated
	// CompressionFailedEmptySummary means the summarizer returned nothing usable.
	CompressionFailedEmptySummary
	// CompressionFailedInflatedTokenCount means the result grew, so it was rejected.
	CompressionFailedInflatedTokenCount
)

// CompressionInfo summarizes a compression pass for telemetry/UI.
type CompressionInfo struct {
	// OriginalTokenCount is the pre-compression token estimate.
	OriginalTokenCount int
	// NewTokenCount is the post-compression token estimate.
	NewTokenCount int
	// Status is the outcome.
	Status CompressionStatus
}

// CompressionResult carries the (possibly nil) new history and its info.
type CompressionResult struct {
	// NewHistory is the compressed history, or nil when unchanged.
	NewHistory []Message
	// Info describes the outcome.
	Info CompressionInfo
}

// Summarizer produces a summary string from the supplied contents using the
// active provider model. systemInstruction is the compression system prompt.
type Summarizer func(ctx context.Context, contents []Message, systemInstruction string) (string, error)

// Compressor summarizes older chat history to fit a context window, using the
// active provider model only (no secondary/per-utility model routing — AD-015).
type Compressor struct {
	// Summarize performs the LLM summarization turns. Required for real compression.
	Summarize Summarizer
	// Estimate overrides the default token estimator.
	Estimate EstimateFn
	// CountRequestTokens overrides the post-compression token counter.
	CountRequestTokens func([]Part) int
	// SaveTruncated offloads a truncated tool output and returns its file path.
	SaveTruncated func(content, toolName string, id int) (string, error)
	// OutputDir is the base directory used by the default SaveTruncated.
	OutputDir string
	// TruncateThreshold is the char budget for budget truncation (default 40000).
	TruncateThreshold int
	// CompressionPrompt is the summarizer system instruction.
	CompressionPrompt string

	nextTruncationID int
}

// CompressOptions configures a single Compress call.
type CompressOptions struct {
	// History is the curated chat history (oldest first).
	History []Message
	// Force compresses regardless of the threshold check.
	Force bool
	// OriginalTokenCount is the pre-turn token estimate of History.
	OriginalTokenCount int
	// Threshold is the compression trigger fraction.
	Threshold float64
	// EffectiveLimit is the context window in tokens.
	EffectiveLimit int
	// PreserveFraction is the fraction of recent history kept raw.
	PreserveFraction float64
	// HasFailedAttempt skips re-summarization after a prior failure.
	HasFailedAttempt bool
}

// Compress compresses history per the fork algorithm. It returns a result whose
// NewHistory is nil unless history actually changed.
func (c *Compressor) Compress(ctx context.Context, opts CompressOptions) (CompressionResult, error) {
	estimate := c.estimator()
	original := opts.OriginalTokenCount

	if len(opts.History) == 0 {
		return noopResult(0), nil
	}
	if !opts.Force && belowThreshold(original, opts.Threshold, opts.EffectiveLimit) {
		return noopResult(original), nil
	}

	truncated, err := c.truncateHistoryToBudget(opts.History)
	if err != nil {
		return CompressionResult{}, err
	}

	if opts.HasFailedAttempt && !opts.Force {
		return c.truncationOnlyResult(truncated, original, estimate), nil
	}

	split, err := FindCompressSplitPoint(truncated, 1-opts.PreserveFraction)
	if err != nil {
		return CompressionResult{}, fmt.Errorf("compression split point: %w", err)
	}
	historyToCompress := truncated[:split]
	historyToKeep := truncated[split:]
	if len(historyToCompress) == 0 {
		return noopResult(original), nil
	}

	summaryHistory := c.summarizerHistory(opts.History, truncated, split, opts.EffectiveLimit, estimate)
	finalSummary, err := c.summarize(ctx, summaryHistory)
	if err != nil {
		return CompressionResult{}, err
	}
	if finalSummary == "" {
		return CompressionResult{Info: CompressionInfo{
			OriginalTokenCount: original,
			NewTokenCount:      original,
			Status:             CompressionFailedEmptySummary,
		}}, nil
	}

	extraHistory := buildCompressedHistory(finalSummary, historyToKeep)
	newTokenCount := c.requestTokenCounter()(flattenParts(extraHistory))

	if newTokenCount > original {
		return CompressionResult{Info: CompressionInfo{
			OriginalTokenCount: original,
			NewTokenCount:      newTokenCount,
			Status:             CompressionFailedInflatedTokenCount,
		}}, nil
	}
	return CompressionResult{
		NewHistory: extraHistory,
		Info: CompressionInfo{
			OriginalTokenCount: original,
			NewTokenCount:      newTokenCount,
			Status:             Compressed,
		},
	}, nil
}

func (c *Compressor) summarize(ctx context.Context, summaryHistory []Message) (string, error) {
	hasSnapshot := historyHasSnapshot(summaryHistory)
	anchor := newSnapshotInstruction
	if hasSnapshot {
		anchor = anchoredSnapshotInstruction
	}

	firstContents := append(cloneHistory(summaryHistory), Message{
		Role:  RoleUser,
		Parts: []Part{{Text: anchor + "\n\nFirst, reason in your scratchpad. Then, generate the updated <state_snapshot>."}},
	})
	summary, err := c.Summarize(ctx, firstContents, c.CompressionPrompt)
	if err != nil {
		return "", fmt.Errorf("summarize history: %w", err)
	}

	verifyContents := append(cloneHistory(summaryHistory),
		Message{Role: RoleModel, Parts: []Part{{Text: summary}}},
		Message{Role: RoleUser, Parts: []Part{{Text: verificationInstruction}}},
	)
	verification, err := c.Summarize(ctx, verifyContents, c.CompressionPrompt)
	if err != nil {
		return "", fmt.Errorf("verify summary: %w", err)
	}

	chosen := strings.TrimSpace(verification)
	if chosen == "" {
		chosen = summary
	}
	return strings.TrimSpace(chosen), nil
}

func (c *Compressor) summarizerHistory(curated, truncated []Message, split, limit int, estimate EstimateFn) []Message {
	originalToCompress := curated[:minInt(split, len(curated))]
	originalTokens := estimate(flattenParts(originalToCompress))
	if originalTokens < limit {
		return originalToCompress
	}
	return truncated[:split]
}

func belowThreshold(original int, threshold float64, limit int) bool {
	if threshold <= 0 {
		threshold = DefaultCompressionTokenThreshold
	}
	return float64(original) < threshold*float64(limit)
}

func (c *Compressor) truncationOnlyResult(truncated []Message, original int, estimate EstimateFn) CompressionResult {
	truncatedTokens := estimate(flattenParts(truncated))
	if truncatedTokens < original {
		return CompressionResult{
			NewHistory: truncated,
			Info: CompressionInfo{
				OriginalTokenCount: original,
				NewTokenCount:      truncatedTokens,
				Status:             ContentTruncated,
			},
		}
	}
	return noopResult(original)
}

// truncateHistoryToBudget applies a reverse token budget to function responses:
// recent tool outputs are kept in full; once the budget is exceeded, older
// large responses are truncated to a file-backed marker.
func (c *Compressor) truncateHistoryToBudget(history []Message) ([]Message, error) {
	estimate := c.estimator()
	counter := 0
	out := make([]Message, len(history))

	for i := len(history) - 1; i >= 0; i-- {
		content := history[i]
		newParts := make([]Part, 0, len(content.Parts))
		for j := len(content.Parts) - 1; j >= 0; j-- {
			part := content.Parts[j]
			if part.FunctionResponse == nil {
				newParts = prepend(newParts, part)
				continue
			}
			contentStr := functionResponseString(part.FunctionResponse)
			tokens := estimate([]Part{{Text: contentStr}})
			if counter+tokens <= compressionFunctionResponseTokenBudget {
				counter += tokens
				newParts = prepend(newParts, part)
				continue
			}
			truncatedPart, addTokens, err := c.truncatePart(part, contentStr, estimate)
			if err != nil {
				newParts = prepend(newParts, part)
				counter += tokens
				continue
			}
			newParts = prepend(newParts, truncatedPart)
			counter += addTokens
		}
		out[i] = Message{Role: content.Role, Parts: newParts}
	}
	return out, nil
}

func (c *Compressor) truncatePart(part Part, contentStr string, estimate EstimateFn) (Part, int, error) {
	c.nextTruncationID++
	outputFile, err := c.saveTruncated()(contentStr, part.FunctionResponse.Name, c.nextTruncationID)
	if err != nil {
		return Part{}, 0, err
	}
	truncatedMessage := formatTruncatedToolOutput(contentStr, outputFile, c.truncateThreshold())
	truncatedPart := Part{FunctionResponse: &FunctionResponse{
		Name:     part.FunctionResponse.Name,
		Response: map[string]any{"output": truncatedMessage},
	}}
	return truncatedPart, estimate([]Part{{Text: truncatedMessage}}), nil
}

func (c *Compressor) estimator() EstimateFn {
	if c.Estimate != nil {
		return c.Estimate
	}
	return EstimateTokens
}

func (c *Compressor) requestTokenCounter() func([]Part) int {
	if c.CountRequestTokens != nil {
		return c.CountRequestTokens
	}
	return EstimateTokens
}

func (c *Compressor) truncateThreshold() int {
	if c.TruncateThreshold > 0 {
		return c.TruncateThreshold
	}
	return DefaultTruncateToolOutputThreshold
}

func (c *Compressor) saveTruncated() func(content, toolName string, id int) (string, error) {
	if c.SaveTruncated != nil {
		return c.SaveTruncated
	}
	return c.defaultSaveTruncated
}

func (c *Compressor) defaultSaveTruncated(content, toolName string, id int) (string, error) {
	dir := c.OutputDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "sagittarius")
	}
	dir = filepath.Join(dir, ToolOutputsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create truncation dir: %w", err)
	}
	name := fmt.Sprintf("%s_%d.txt", strings.ToLower(sanitizeFilenamePart(toolName)), id)
	filePath := filepath.Join(dir, name)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write truncation offload: %w", err)
	}
	return filePath, nil
}

// FindCompressSplitPoint returns the index of the oldest item to keep when
// compressing. It may return len(contents), meaning compress everything.
// fraction must be strictly between 0 and 1.
func FindCompressSplitPoint(contents []Message, fraction float64) (int, error) {
	if fraction <= 0 || fraction >= 1 {
		return 0, fmt.Errorf("fraction must be between 0 and 1")
	}

	charCounts := make([]int, len(contents))
	total := 0
	for i := range contents {
		charCounts[i] = contentCharCount(contents[i])
		total += charCounts[i]
	}
	target := float64(total) * fraction

	lastSplitPoint := 0
	cumulative := 0.0
	for i := range contents {
		if contents[i].Role == RoleUser && !hasFunctionResponse(contents[i]) {
			if cumulative >= target {
				return i, nil
			}
			lastSplitPoint = i
		}
		cumulative += float64(charCounts[i])
	}

	if len(contents) > 0 {
		last := contents[len(contents)-1]
		if last.Role == RoleModel && !hasFunctionCall(last) {
			return len(contents), nil
		}
	}
	return lastSplitPoint, nil
}

// formatTruncatedToolOutput shows the first 20% and last 80% of maxChars with a
// marker in between. Ported verbatim from fileUtils.formatTruncatedToolOutput,
// including the thousands-separated counts used by the fork's golden tests.
func formatTruncatedToolOutput(contentStr, outputFile string, maxChars int) string {
	if len(contentStr) <= maxChars {
		return contentStr
	}
	headChars := int(math.Floor(float64(maxChars) * truncateHeadFraction))
	tailChars := maxChars - headChars
	head := contentStr[:headChars]
	tail := contentStr[len(contentStr)-tailChars:]
	omitted := len(contentStr) - headChars - tailChars
	return fmt.Sprintf(
		"Output too large. Showing first %s and last %s characters. For full output see: %s\n%s\n\n... [%s characters omitted] ...\n\n%s",
		withThousands(headChars), withThousands(tailChars), outputFile, head, withThousands(omitted), tail,
	)
}

func functionResponseString(fr *FunctionResponse) string {
	if fr.Response == nil {
		return "null"
	}
	if v, ok := fr.Response["output"].(string); ok {
		return v
	}
	if v, ok := fr.Response["content"].(string); ok {
		return v
	}
	b, err := json.MarshalIndent(fr.Response, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func buildCompressedHistory(summary string, keep []Message) []Message {
	out := make([]Message, 0, len(keep)+2)
	out = append(out,
		Message{Role: RoleUser, Parts: []Part{{Text: summary}}},
		Message{Role: RoleModel, Parts: []Part{{Text: "Got it. Thanks for the additional context!"}}},
	)
	return append(out, keep...)
}

func historyHasSnapshot(history []Message) bool {
	for i := range history {
		for j := range history[i].Parts {
			if strings.Contains(history[i].Parts[j].Text, "<state_snapshot>") {
				return true
			}
		}
	}
	return false
}

func hasFunctionResponse(content Message) bool {
	return containsFunctionResponse(content)
}

func hasFunctionCall(content Message) bool {
	return containsFunctionCall(content)
}

func flattenParts(history []Message) []Part {
	var parts []Part
	for i := range history {
		parts = append(parts, history[i].Parts...)
	}
	return parts
}

func prepend(parts []Part, p Part) []Part {
	return append([]Part{p}, parts...)
}

func noopResult(tokens int) CompressionResult {
	return CompressionResult{Info: CompressionInfo{
		OriginalTokenCount: tokens,
		NewTokenCount:      tokens,
		Status:             CompressionNoOp,
	}}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// withThousands formats n with comma separators (e.g. 32000 -> "32,000"),
// matching JS Number.toLocaleString for the en-US locale used by the fork.
func withThousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out bytes.Buffer
	for i, digit := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(digit)
	}
	if neg {
		return "-" + out.String()
	}
	return out.String()
}
