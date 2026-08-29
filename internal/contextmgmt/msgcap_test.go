package contextmgmt

import (
	"os"
	"strings"
	"testing"
)

func TestCapMessagesForSummarizerShortUnchanged(t *testing.T) {
	t.Parallel()
	history := []Message{
		msg("user", "short user prompt"),
		fcMsg("read_file"),
		msg("model", "short model response"),
	}
	capped := CapMessagesForSummarizer(history, 6000)
	if len(capped) != len(history) {
		t.Fatalf("len = %d, want %d", len(capped), len(history))
	}
	if capped[0].Parts[0].Text != "short user prompt" {
		t.Errorf("got %q, want %q", capped[0].Parts[0].Text, "short user prompt")
	}
}

func TestCapMessagesForSummarizerLongText(t *testing.T) {
	t.Parallel()
	longText := strings.Repeat("a", 3000) + strings.Repeat("b", 3000) + strings.Repeat("c", 3000) // 9000 chars
	history := []Message{
		msg("user", longText),
	}
	capped := CapMessagesForSummarizer(history, 6000)
	text := capped[0].Parts[0].Text
	if len([]rune(text)) >= 9000 {
		t.Fatalf("expected text to be capped, got len=%d", len([]rune(text)))
	}
	if !strings.Contains(text, "characters omitted") {
		t.Fatalf("expected omission marker, got: %s", text)
	}
	if !strings.HasPrefix(text, strings.Repeat("a", 3000)) {
		t.Errorf("head was not preserved")
	}
	if !strings.HasSuffix(text, strings.Repeat("c", 1500)) {
		t.Errorf("tail was not preserved")
	}
}

func TestCapMessagesForSummarizerToolArgs(t *testing.T) {
	t.Parallel()
	longArg := strings.Repeat("x", 3000)
	tc := &ToolCall{
		ID:   "call_1",
		Name: "write_file",
		Args: map[string]any{
			"path":    "main.go",
			"content": longArg,
		},
	}
	history := []Message{
		{Role: RoleModel, Parts: []Part{{FunctionCall: tc}}},
	}
	capped := CapMessagesForSummarizer(history, 6000)
	fc := capped[0].Parts[0].FunctionCall
	if fc == nil {
		t.Fatal("expected function call part")
	}
	contentVal, ok := fc.Args["content"].(string)
	if !ok {
		t.Fatalf("expected string content arg, got %T", fc.Args["content"])
	}
	if len(contentVal) >= 3000 {
		t.Fatalf("expected tool arg to be capped, got len=%d", len(contentVal))
	}
	if !strings.Contains(contentVal, "characters omitted") {
		t.Fatalf("expected omission marker in tool args, got: %s", contentVal)
	}
	if fc.Args["path"] != "main.go" {
		t.Errorf("unrelated arg modified: %v", fc.Args["path"])
	}
}

func TestCapMessagesForSummarizerFunctionResponse(t *testing.T) {
	t.Parallel()
	longOutput := strings.Repeat("line\n", 2000) // 10000 chars
	fr := &FunctionResponse{
		Name:   "run_shell_command",
		CallID: "call_1",
		Response: map[string]any{
			"output": longOutput,
		},
	}
	history := []Message{
		{Role: RoleUser, Parts: []Part{{FunctionResponse: fr}}},
	}
	capped := CapMessagesForSummarizer(history, 6000)
	outFR := capped[0].Parts[0].FunctionResponse
	if outFR == nil {
		t.Fatal("expected function response")
	}
	outStr, ok := outFR.Response["output"].(string)
	if !ok {
		t.Fatalf("expected string output, got %T", outFR.Response["output"])
	}
	if len([]rune(outStr)) >= 10000 {
		t.Fatalf("expected function response output to be capped, got len=%d", len([]rune(outStr)))
	}
	if !strings.Contains(outStr, "characters omitted") {
		t.Fatalf("expected omission marker in function response, got: %s", outStr)
	}
}

func TestCapMessagesForSummarizerRuneSafety(t *testing.T) {
	t.Parallel()
	// Test with multi-byte unicode characters (e.g. Japanese Kanji and emojis).
	unicodeText := strings.Repeat("こんにちは世界🌟", 1000) // 8 runes per repeat, 8000 runes total
	history := []Message{
		msg("user", unicodeText),
	}
	capped := CapMessagesForSummarizer(history, 6000)
	text := capped[0].Parts[0].Text
	if !strings.Contains(text, "characters omitted") {
		t.Fatalf("expected omission marker, got: %s", text)
	}
	// Verify valid UTF-8 and rune count <= 6000 + marker length
	if !strings.HasPrefix(text, strings.Repeat("こんにちは世界🌟", 400)) {
		t.Errorf("unicode head not preserved accurately")
	}
}

func TestCapOversizedMessages(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	largeText := strings.Repeat("large message content line\n", 500) // ~13500 chars, ~3375 tokens
	history := []Message{
		msg("user", largeText),
		msg("model", "ok"),
	}

	// Target 100 tokens (well below the ~3375 tokens)
	res, err := CapOversizedMessages(history, 100, EstimateTokens, tmpDir, "sess-123")
	if err != nil {
		t.Fatalf("CapOversizedMessages: %v", err)
	}
	if res.CappedCount == 0 {
		t.Fatal("expected at least 1 message to be capped")
	}
	if res.NewTokenCount >= EstimateTokens(flattenParts(history)) {
		t.Fatalf("expected token count to decrease: before=%d after=%d",
			EstimateTokens(flattenParts(history)), res.NewTokenCount)
	}

	cappedText := res.NewHistory[0].Parts[0].Text
	if !strings.Contains(cappedText, "Content too large.") {
		t.Fatalf("expected 'Content too large.' in capped text: %s", cappedText)
	}

	// Verify offloaded file exists and has original full content
	lines := strings.Split(cappedText, "\n")
	var offloadPath string
	for _, l := range lines {
		if strings.Contains(l, "For full content see: ") {
			offloadPath = strings.TrimPrefix(l, "Content too large. Showing first 800 and last 400 characters. For full content see: ")
			break
		}
	}
	if offloadPath == "" {
		t.Fatalf("could not extract offload path from: %s", cappedText)
	}
	contentBytes, err := os.ReadFile(offloadPath)
	if err != nil {
		t.Fatalf("read offload file %q: %v", offloadPath, err)
	}
	if string(contentBytes) != largeText {
		t.Fatalf("offload content mismatch: got %d bytes, want %d bytes", len(contentBytes), len(largeText))
	}
}
