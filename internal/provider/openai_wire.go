package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// OpenAIRole is the role field on an OpenAI chat message.
type OpenAIRole string

const (
	OpenAIRoleSystem    OpenAIRole = "system"
	OpenAIRoleUser      OpenAIRole = "user"
	OpenAIRoleAssistant OpenAIRole = "assistant"
	OpenAIRoleTool      OpenAIRole = "tool"
)

// OpenAIMessage is one message in an OpenAI chat completions request.
type OpenAIMessage struct {
	Role       OpenAIRole       `json:"role"`
	Content    *string          `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string           `json:"type"`
	Function openAIToolSchema `json:"function"`
}

type openAIToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIChatRequest struct {
	Model         string          `json:"model"`
	Messages      []OpenAIMessage `json:"messages"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
	Tools         []openAITool    `json:"tools,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	MaxTokens     *int32          `json:"max_tokens,omitempty"`
	Stop          []string        `json:"stop,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Choices []openAIStreamChoice `json:"choices"`
	Usage   *openAIUsage         `json:"usage,omitempty"`
}

type openAIStreamChoice struct {
	Index        int               `json:"index"`
	Delta        openAIStreamDelta `json:"delta"`
	FinishReason *string           `json:"finish_reason"`
}

type openAIStreamDelta struct {
	Content          *string                 `json:"content"`
	ReasoningContent *string                 `json:"reasoning_content"`
	Reasoning        *string                 `json:"reasoning"`
	ToolCalls        []openAIStreamDeltaTool `json:"tool_calls"`
}

type openAIStreamDeltaTool struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function *openAIStreamDeltaFunc `json:"function,omitempty"`
}

type openAIStreamDeltaFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int      `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	Cost             *float64 `json:"cost,omitempty"` // OpenRouter: actual USD cost for this request
}

type openAINonStreamResponse struct {
	ID      string                  `json:"id"`
	Choices []openAINonStreamChoice `json:"choices"`
}

type openAINonStreamChoice struct {
	Message      openAINonStreamMessage `json:"message"`
	FinishReason string                 `json:"finish_reason"`
}

type openAINonStreamMessage struct {
	Role      string           `json:"role"`
	Content   *string          `json:"content"`
	ToolCalls []openAIToolCall `json:"tool_calls"`
}

type openAIModelsResponse struct {
	Data []openAIModelEntry `json:"data"`
}

type openAIModelEntry struct {
	ID string `json:"id"`
	// ContextLength is OpenRouter's per-model window; MaxModelLen is the
	// vLLM/OpenAI-compat field. Either may be absent (0).
	ContextLength int `json:"context_length"`
	MaxModelLen   int `json:"max_model_len"`
}

const (
	mistralToolUserBridgeContent = "."
	orphanedToolResponseContent  = `{"error":"Tool call interrupted — no response recorded."}`
)

var (
	mistralFamilyRE   = regexp.MustCompile(`(?i)(mistral|devstral|mixtral|codestral|magistral|ministral)`)
	wrappedToolCallRE = regexp.MustCompile(`(?s)<tool_call>\s*(.*?)\s*</tool_call>`)
	innerFunctionRE   = regexp.MustCompile(`(?s)<function=(\w+)>(.*?)</function>`)
	bareFunctionRE    = regexp.MustCompile(`(?s)<function=(\w+)>(.*?)</function>`)
	parameterRE       = regexp.MustCompile(`(?s)<parameter=(\w+)>(.*?)</parameter>`)
)

// IsMistralFamilyModel reports whether modelID belongs to the Mistral model family.
func IsMistralFamilyModel(modelID string) bool {
	return mistralFamilyRE.MatchString(modelID)
}

// BuildNonStreamRetryBody strips stream_options and forces stream=false for retry.
func BuildNonStreamRetryBody(original map[string]any) map[string]any {
	out := make(map[string]any, len(original))
	for k, v := range original {
		if k == "stream_options" {
			continue
		}
		out[k] = v
	}
	out["stream"] = false
	return out
}

// ParseXMLToolCalls extracts Qwen-style XML tool calls from assistant content.
func ParseXMLToolCalls(content string, mode config.ToolCallParsingMode) []openAIToolCall {
	effective := mode
	if effective == "" {
		effective = config.ToolCallParsingLenient
	}

	var calls []openAIToolCall
	switch effective {
	case config.ToolCallParsingLoose:
		calls = matchAllFunctionBlocks(content)
	default:
		wrapped := matchWrappedToolCalls(content)
		if effective == config.ToolCallParsingStrict || !hasOrphanedToolCallCloser(content) {
			calls = wrapped
		} else {
			bare := matchBareFunctionBlocksOutsideWrappers(content)
			calls = append(wrapped, bare...)
			for i := range calls {
				calls[i].ID = fmt.Sprintf("call_xml_%d", i)
			}
		}
	}
	return calls
}

func matchWrappedToolCalls(content string) []openAIToolCall {
	var calls []openAIToolCall
	for _, wrapper := range wrappedToolCallRE.FindAllStringSubmatch(content, -1) {
		body := wrapper[1]
		for _, fn := range innerFunctionRE.FindAllStringSubmatch(body, -1) {
			calls = append(calls, functionMatchToToolCall(fn[1], fn[2], len(calls)))
		}
	}
	return calls
}

func matchAllFunctionBlocks(content string) []openAIToolCall {
	var calls []openAIToolCall
	for _, fn := range bareFunctionRE.FindAllStringSubmatch(content, -1) {
		calls = append(calls, functionMatchToToolCall(fn[1], fn[2], len(calls)))
	}
	return calls
}

func matchBareFunctionBlocksOutsideWrappers(content string) []openAIToolCall {
	stripped := wrappedToolCallRE.ReplaceAllStringFunc(content, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
	return matchAllFunctionBlocks(stripped)
}

func hasOrphanedToolCallCloser(content string) bool {
	openers := strings.Count(content, "<tool_call>")
	closers := strings.Count(content, "</tool_call>")
	return closers > openers
}

func functionMatchToToolCall(funcName, paramsBody string, idIndex int) openAIToolCall {
	args := map[string]string{}
	for _, m := range parameterRE.FindAllStringSubmatch(paramsBody, -1) {
		args[m[1]] = strings.TrimSpace(m[2])
	}
	argJSON, _ := json.Marshal(args)
	return openAIToolCall{
		ID:   fmt.Sprintf("call_xml_%d", idIndex),
		Type: "function",
		Function: openAIFunctionCall{
			Name:      funcName,
			Arguments: string(argJSON),
		},
	}
}

func patchToolUserTransitionForMistral(messages []OpenAIMessage, modelID string) []OpenAIMessage {
	if !IsMistralFamilyModel(modelID) {
		return messages
	}
	out := make([]OpenAIMessage, 0, len(messages)+1)
	for i, current := range messages {
		out = append(out, current)
		if i+1 < len(messages) &&
			current.Role == OpenAIRoleTool &&
			messages[i+1].Role == OpenAIRoleUser {
			bridge := mistralToolUserBridgeContent
			out = append(out, OpenAIMessage{Role: OpenAIRoleAssistant, Content: &bridge})
		}
	}
	return out
}

func patchOrphanedToolCallsForMistral(messages []OpenAIMessage, modelID string) []OpenAIMessage {
	if !IsMistralFamilyModel(modelID) {
		return messages
	}
	out := make([]OpenAIMessage, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		current := messages[i]
		if current.Role != OpenAIRoleAssistant || len(current.ToolCalls) == 0 {
			out = append(out, current)
			continue
		}
		out = append(out, current)
		i++
		responded := map[string]struct{}{}
		for i < len(messages) && messages[i].Role == OpenAIRoleTool {
			toolMsg := messages[i]
			if toolMsg.ToolCallID != "" {
				responded[toolMsg.ToolCallID] = struct{}{}
			}
			out = append(out, toolMsg)
			i++
		}
		i--
		for _, tc := range current.ToolCalls {
			if _, ok := responded[tc.ID]; ok {
				continue
			}
			content := orphanedToolResponseContent
			out = append(out, OpenAIMessage{
				Role:       OpenAIRoleTool,
				ToolCallID: tc.ID,
				Content:    &content,
			})
		}
	}
	return out
}

type pendingToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

type sseStreamState struct {
	pending       map[int]*pendingToolCall
	contentBuffer strings.Builder
	lastFinish    string
	lastResponse  string
	lastUsage     *openAIUsage
}

func parseSSEStream(
	r io.Reader,
	parseMode config.ToolCallParsingMode,
	onChunk func(StreamResponse) bool,
) (needsNonStreamRetry bool, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	state := &sseStreamState{pending: map[int]*pendingToolCall{}}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if line == "data: [DONE]" {
			return flushSSEState(state, parseMode, onChunk)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			continue
		}
		// Capture usage from any chunk (OpenRouter sends a usage-only final frame
		// with no choices; we must not skip it).
		if chunk.Usage != nil {
			state.lastUsage = chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if chunk.ID != "" {
			state.lastResponse = chunk.ID
		}
		if choice.FinishReason != nil {
			state.lastFinish = *choice.FinishReason
		}

		if len(choice.Delta.ToolCalls) > 0 {
			for _, tc := range choice.Delta.ToolCalls {
				existing, ok := state.pending[tc.Index]
				if !ok {
					existing = &pendingToolCall{id: tc.ID}
					if existing.id == "" {
						existing.id = fmt.Sprintf("call_%d", tc.Index)
					}
					state.pending[tc.Index] = existing
				}
				if tc.ID != "" {
					existing.id = tc.ID
				}
				if tc.Function != nil {
					if tc.Function.Name != "" {
						existing.name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						existing.arguments.WriteString(tc.Function.Arguments)
					}
				}
			}
			continue
		}

		deltaText := deltaContent(choice.Delta)
		if deltaText != "" {
			state.contentBuffer.WriteString(deltaText)
			if !onChunk(StreamResponse{TextDelta: deltaText}) {
				return false, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return false, err
	}
	return flushSSEState(state, parseMode, onChunk)
}

func flushSSEState(
	state *sseStreamState,
	parseMode config.ToolCallParsingMode,
	onChunk func(StreamResponse) bool,
) (needsNonStreamRetry bool, err error) {
	if len(state.pending) > 0 {
		calls := pendingToToolCalls(state.pending)
		if !onChunk(StreamResponse{ToolCalls: calls}) {
			return false, nil
		}
	} else if state.lastFinish == "tool_calls" {
		xmlCalls := openAIToolCallsToDomain(ParseXMLToolCalls(state.contentBuffer.String(), parseMode))
		if len(xmlCalls) > 0 {
			if !onChunk(StreamResponse{ToolCalls: xmlCalls}) {
				return false, nil
			}
		} else {
			needsNonStreamRetry = true
		}
	}
	if needsNonStreamRetry {
		return true, nil
	}
	// Emit provider-reported usage (and optional OpenRouter cost) before Done.
	if state.lastUsage != nil {
		u := &Usage{
			InputTokens:  state.lastUsage.PromptTokens,
			OutputTokens: state.lastUsage.CompletionTokens,
		}
		if state.lastUsage.Cost != nil {
			u.CostUSD = *state.lastUsage.Cost
			u.CostKnown = true
		}
		if !onChunk(StreamResponse{Usage: u}) {
			return false, nil
		}
	}
	if !onChunk(StreamResponse{Done: true}) {
		return false, nil
	}
	return false, nil
}

func deltaContent(delta openAIStreamDelta) string {
	if delta.Content != nil {
		return *delta.Content
	}
	if delta.ReasoningContent != nil {
		return *delta.ReasoningContent
	}
	if delta.Reasoning != nil {
		return *delta.Reasoning
	}
	return ""
}

func pendingToToolCalls(pending map[int]*pendingToolCall) []ToolCall {
	indices := make([]int, 0, len(pending))
	for idx := range pending {
		indices = append(indices, idx)
	}
	for i := 0; i < len(indices); i++ {
		for j := i + 1; j < len(indices); j++ {
			if indices[j] < indices[i] {
				indices[i], indices[j] = indices[j], indices[i]
			}
		}
	}
	out := make([]ToolCall, 0, len(indices))
	for _, idx := range indices {
		tc := pending[idx]
		out = append(out, openAIToolCallToDomain(openAIToolCall{
			ID:   tc.id,
			Type: "function",
			Function: openAIFunctionCall{
				Name:      tc.name,
				Arguments: tc.arguments.String(),
			},
		}))
	}
	return out
}

func openAIToolCallToDomain(tc openAIToolCall) ToolCall {
	args := map[string]any{}
	if tc.Function.Arguments != "" {
		_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
	}
	if args == nil {
		args = map[string]any{}
	}
	return ToolCall{
		ID:   tc.ID,
		Name: tc.Function.Name,
		Args: args,
	}
}

func openAIToolCallsToDomain(calls []openAIToolCall) []ToolCall {
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, openAIToolCallToDomain(call))
	}
	return out
}

func decodeNonStreamResponse(body []byte, parseMode config.ToolCallParsingMode) ([]StreamResponse, error) {
	var resp openAINonStreamResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return []StreamResponse{{Done: true}}, nil
	}
	choice := resp.Choices[0]
	var chunks []StreamResponse
	if choice.Message.Content != nil && *choice.Message.Content != "" {
		chunks = append(chunks, StreamResponse{TextDelta: *choice.Message.Content})
	}
	if len(choice.Message.ToolCalls) > 0 {
		chunks = append(chunks, StreamResponse{ToolCalls: openAIToolCallsToDomain(choice.Message.ToolCalls)})
	} else if choice.FinishReason == "tool_calls" && choice.Message.Content != nil {
		xmlCalls := openAIToolCallsToDomain(ParseXMLToolCalls(*choice.Message.Content, parseMode))
		if len(xmlCalls) > 0 {
			chunks = append(chunks, StreamResponse{ToolCalls: xmlCalls})
		}
	}
	chunks = append(chunks, StreamResponse{Done: true})
	return chunks, nil
}

func encodeChatRequestBody(req openAIChatRequest) ([]byte, error) {
	return json.Marshal(req)
}

func decodeChatRequestBody(data []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func cloneRequestBodyForRetry(streamBody []byte) ([]byte, error) {
	raw, err := decodeChatRequestBody(streamBody)
	if err != nil {
		return nil, err
	}
	retry := BuildNonStreamRetryBody(raw)
	return json.Marshal(retry)
}

func isSSEContentType(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	return strings.HasPrefix(ct, "text/event-stream") ||
		strings.HasPrefix(ct, "application/json") ||
		ct == ""
}

func readBodyPreview(r io.Reader, limit int) string {
	buf, err := io.ReadAll(io.LimitReader(r, int64(limit)))
	if err != nil {
		return "<unreadable>"
	}
	s := string(buf)
	if len(s) >= limit {
		return s[:limit] + "…"
	}
	return s
}

func stripBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}
