package contextmgmt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SummarizerMessageMaxChars is the default maximum character count for any
	// single message body sent to the compression summarizer.
	SummarizerMessageMaxChars = 6000
	// SummarizerMessageHeadChars is the head portion kept when capping.
	SummarizerMessageHeadChars = 4000
	// SummarizerMessageTailChars is the tail portion kept when capping.
	SummarizerMessageTailChars = 1500

	// SummarizerToolArgsMaxChars is the maximum character count for tool call arguments.
	SummarizerToolArgsMaxChars = 1500
	// SummarizerToolArgsHeadChars is the head portion for tool call args.
	SummarizerToolArgsHeadChars = 1200
	// SummarizerToolArgsTailChars is the tail portion for tool call args.
	SummarizerToolArgsTailChars = 200
)

// CapMessagesForSummarizer returns a copy of history where oversized message parts
// (text, tool arguments, tool responses) are capped to head/tail previews with an
// inline omission marker.
//
// Godoc note on AD-068 safety:
// Mutating historical tool calls or message contents IN LIVE CHAT HISTORY is
// prohibited by AD-068 because models mimic prior tool call shapes in the next turn.
// CapMessagesForSummarizer is used EXCLUSIVELY to format the transient, one-off
// prompt sent to the summarizer LLM to produce a <state_snapshot>. The capped
// message slice is never stored in session history, never recorded in JSONL, and
// never returned to the main chat model. Capping here bounds summarizer input size
// without any risk of model mimicry.
func CapMessagesForSummarizer(history []Message, maxChars int) []Message {
	if len(history) == 0 {
		return nil
	}
	if maxChars <= 0 {
		maxChars = SummarizerMessageMaxChars
	}
	headChars := SummarizerMessageHeadChars
	tailChars := SummarizerMessageTailChars
	if maxChars != SummarizerMessageMaxChars {
		headChars = int(math.Floor(float64(maxChars) * (4.0 / 6.0)))
		tailChars = int(math.Floor(float64(maxChars) * (1.5 / 6.0)))
		if headChars+tailChars >= maxChars {
			headChars = maxChars / 2
			tailChars = maxChars / 4
		}
	}

	out := make([]Message, len(history))
	for i, msg := range history {
		newParts := make([]Part, len(msg.Parts))
		for j, p := range msg.Parts {
			newParts[j] = capPartForSummarizer(p, maxChars, headChars, tailChars)
		}
		out[i] = Message{Role: msg.Role, Parts: newParts}
	}
	return out
}

func capPartForSummarizer(p Part, maxChars, headChars, tailChars int) Part {
	res := p
	if p.Text != "" {
		if head, tail, omitted, capped := runeHeadTail(p.Text, maxChars, headChars, tailChars); capped {
			res.Text = fmt.Sprintf("%s\n\n... [%s characters omitted] ...\n\n%s", head, withThousands(omitted), tail)
		}
	}
	if p.FunctionCall != nil && len(p.FunctionCall.Args) > 0 {
		res.FunctionCall = capToolCallArgs(p.FunctionCall)
	}
	if p.FunctionResponse != nil && len(p.FunctionResponse.Response) > 0 {
		res.FunctionResponse = capFunctionResponse(p.FunctionResponse, maxChars, headChars, tailChars)
	}
	return res
}

func capToolCallArgs(tc *ToolCall) *ToolCall {
	if tc == nil || len(tc.Args) == 0 {
		return tc
	}
	newArgs := make(map[string]any, len(tc.Args))
	changed := false
	for k, v := range tc.Args {
		if s, ok := v.(string); ok {
			if head, tail, omitted, capped := runeHeadTail(s, SummarizerToolArgsMaxChars, SummarizerToolArgsHeadChars, SummarizerToolArgsTailChars); capped {
				newArgs[k] = fmt.Sprintf("%s... [%s characters omitted] ...%s", head, withThousands(omitted), tail)
				changed = true
				continue
			}
		}
		newArgs[k] = v
	}
	if !changed {
		return tc
	}
	return &ToolCall{
		ID:   tc.ID,
		Name: tc.Name,
		Args: newArgs,
	}
}

func capFunctionResponse(fr *FunctionResponse, maxChars, headChars, tailChars int) *FunctionResponse {
	if fr == nil || len(fr.Response) == 0 {
		return fr
	}
	newResp := make(map[string]any, len(fr.Response))
	changed := false
	for k, v := range fr.Response {
		if s, ok := v.(string); ok {
			if head, tail, omitted, capped := runeHeadTail(s, maxChars, headChars, tailChars); capped {
				newResp[k] = fmt.Sprintf("%s\n\n... [%s characters omitted] ...\n\n%s", head, withThousands(omitted), tail)
				changed = true
				continue
			}
		}
		newResp[k] = v
	}
	if !changed {
		return fr
	}
	return &FunctionResponse{
		Name:     fr.Name,
		CallID:   fr.CallID,
		Response: newResp,
	}
}

func runeHeadTail(s string, maxChars, headChars, tailChars int) (head string, tail string, omitted int, capped bool) {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, "", 0, false
	}
	if headChars+tailChars >= len(runes) {
		return s, "", 0, false
	}
	head = string(runes[:headChars])
	tail = string(runes[len(runes)-tailChars:])
	omitted = len(runes) - headChars - tailChars
	return head, tail, omitted, true
}

// CapResult reports the result of CapOversizedMessages.
type CapResult struct {
	NewHistory    []Message
	CappedCount   int
	NewTokenCount int
}

// CapOversizedMessages reduces the size of oversized messages in history by writing
// their full content to an offload file and replacing their in-memory representation
// with a head/tail excerpt pointing to the offloaded file. It is the final safety
// rung of the pre-flight request budget ladder.
func CapOversizedMessages(history []Message, targetTokens int, estimate EstimateFn, outputDir, sessionID string) (CapResult, error) {
	if estimate == nil {
		estimate = EstimateTokens
	}
	currentTokens := estimateHistoryTokens(history, estimate)
	if currentTokens <= targetTokens || len(history) == 0 {
		return CapResult{
			NewHistory:    cloneHistory(history),
			CappedCount:   0,
			NewTokenCount: currentTokens,
		}, nil
	}

	out := cloneHistory(history)
	cappedCount := 0

	// Walk from oldest to newest, capping messages that exceed 4000 characters.
	const capThresholdChars = 4000
	const keepHeadChars = 800
	const keepTailChars = 400

	for i := range out {
		if currentTokens <= targetTokens {
			break
		}
		msg := out[i]
		newParts := make([]Part, len(msg.Parts))
		msgCapped := false
		for j, p := range msg.Parts {
			if p.Text != "" && len([]rune(p.Text)) > capThresholdChars {
				file, err := WriteOffloadFile(outputDir, sessionID, "text", p.Text)
				if err == nil {
					head, tail, omitted, _ := runeHeadTail(p.Text, capThresholdChars, keepHeadChars, keepTailChars)
					newParts[j] = Part{
						Text: fmt.Sprintf("Content too large. Showing first %s and last %s characters. For full content see: %s\n%s\n\n... [%s characters omitted] ...\n\n%s",
							withThousands(keepHeadChars), withThousands(keepTailChars), file, head, withThousands(omitted), tail),
						ThoughtSignature: p.ThoughtSignature,
					}
					msgCapped = true
					continue
				}
			}
			if p.FunctionResponse != nil {
				contentStr := functionResponseString(p.FunctionResponse)
				if len([]rune(contentStr)) > capThresholdChars {
					file, err := WriteOffloadFile(outputDir, sessionID, p.FunctionResponse.Name, contentStr)
					if err == nil {
						head, tail, omitted, _ := runeHeadTail(contentStr, capThresholdChars, keepHeadChars, keepTailChars)
						truncatedMsg := fmt.Sprintf("Output too large. Showing first %s and last %s characters. For full output see: %s\n%s\n\n... [%s characters omitted] ...\n\n%s",
							withThousands(keepHeadChars), withThousands(keepTailChars), file, head, withThousands(omitted), tail)
						newParts[j] = Part{
							FunctionResponse: &FunctionResponse{
								Name:     p.FunctionResponse.Name,
								CallID:   p.FunctionResponse.CallID,
								Response: map[string]any{"output": truncatedMsg},
							},
							ThoughtSignature: p.ThoughtSignature,
						}
						msgCapped = true
						continue
					}
				}
			}
			newParts[j] = p
		}
		if msgCapped {
			out[i] = Message{Role: msg.Role, Parts: newParts}
			cappedCount++
			currentTokens = estimateHistoryTokens(out, estimate)
		}
	}

	return CapResult{
		NewHistory:    out,
		CappedCount:   cappedCount,
		NewTokenCount: currentTokens,
	}, nil
}

// WriteOffloadFile writes content to a file in outputDir/ToolOutputsDir/[session-id/]
// and returns the absolute file path.
func WriteOffloadFile(outputDir, sessionID, prefix, content string) (string, error) {
	dir := outputDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "sagittarius")
	}
	dir = filepath.Join(dir, ToolOutputsDir)
	if sessionID != "" {
		dir = filepath.Join(dir, "session-"+sanitizeFilenamePart(sessionID))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create offload dir: %w", err)
	}

	cleanPrefix := strings.ToLower(sanitizeFilenamePart(prefix))
	if cleanPrefix == "" {
		cleanPrefix = "offload"
	}
	randSuffix := randomHexSuffix()
	fileName := fmt.Sprintf("%s_%s.txt", cleanPrefix, randSuffix)
	filePath := filepath.Join(dir, fileName)

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write offload file: %w", err)
	}
	return filePath, nil
}

func randomHexSuffix() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}
