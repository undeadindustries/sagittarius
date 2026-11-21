package provider

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	// AnthropicAPIVersion is the Messages/Models anthropic-version header sent
	// on every native Anthropic request.
	AnthropicAPIVersion = "2026-10-21"

	anthropicDefaultMaxTokens          = 32_000
	anthropicDefaultThinkingBudget     = 10_000
	anthropicMinThinkingBudget         = 1024
	anthropicThinkingAnswerHeadroom    = 4096
	anthropicCacheControlTypeEphemeral = "ephemeral"
)

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	// Text is used by type="text".
	Text string `json:"text,omitempty"`
	// Thinking and Signature are used by type="thinking". Pointer lets an
	// omitted-display thinking block serialize an explicit empty "thinking": "".
	Thinking  *string `json:"thinking,omitempty"`
	Signature string  `json:"signature,omitempty"`
	// Data is used by type="redacted_thinking".
	Data string `json:"data,omitempty"`
	// ID, Name, and Input are used by type="tool_use".
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// ToolUseID, Content, and IsError are used by type="tool_result".
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   string `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicRequestBody struct {
	Model         string                   `json:"model"`
	MaxTokens     int                      `json:"max_tokens"`
	System        []anthropicSystemBlock   `json:"system,omitempty"`
	Messages      []anthropicMessage       `json:"messages"`
	Tools         []anthropicTool          `json:"tools,omitempty"`
	Stream        bool                     `json:"stream"`
	CacheControl  *anthropicCacheControl   `json:"cache_control,omitempty"`
	Thinking      *anthropicThinkingConfig `json:"thinking,omitempty"`
	OutputConfig  *anthropicOutputConfig   `json:"output_config,omitempty"`
	StopSequences []string                 `json:"stop_sequences,omitempty"`
}

// AnthropicPlanOptions configures how BuildAnthropicRequestPlan encodes a
// GenerateRequest for Anthropic Messages.
type AnthropicPlanOptions struct {
	ToolsEnabled         bool
	SystemPromptOverride string
	DefaultMaxTokens     int
	ThinkingTypes        []string
	SupportedEfforts     []string
}

// AnthropicRequestPlan is the wire-ready Anthropic Messages payload.
type AnthropicRequestPlan struct {
	Body anthropicRequestBody
}

// BuildAnthropicRequestPlan translates a neutral GenerateRequest to an
// Anthropic Messages API payload. Sampling parameters (temperature, top_p,
// top_k) are intentionally omitted because API version 2026-10-21 rejects them.
func BuildAnthropicRequestPlan(req *GenerateRequest, opts AnthropicPlanOptions) AnthropicRequestPlan {
	if req == nil {
		return AnthropicRequestPlan{}
	}

	sysText := strings.TrimSpace(req.SystemInstruction)
	if override := strings.TrimSpace(opts.SystemPromptOverride); override != "" {
		sysText = override
	}

	var systemBlocks []anthropicSystemBlock
	if sysText != "" {
		systemBlocks = []anthropicSystemBlock{{
			Type:         "text",
			Text:         sysText,
			CacheControl: &anthropicCacheControl{Type: anthropicCacheControlTypeEphemeral},
		}}
	}

	var tools []anthropicTool
	if opts.ToolsEnabled && len(req.Tools) > 0 {
		tools = ToolDeclarationsToAnthropic(req.Tools)
	}

	thinking, outputCfg := resolveAnthropicThinking(req, opts)

	maxTokens := opts.DefaultMaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}
	if req.MaxOutputTokens != nil && *req.MaxOutputTokens > 0 {
		maxTokens = int(*req.MaxOutputTokens)
	}
	if thinking != nil && thinking.BudgetTokens > 0 && maxTokens <= thinking.BudgetTokens {
		maxTokens = thinking.BudgetTokens + anthropicThinkingAnswerHeadroom
	}

	return AnthropicRequestPlan{
		Body: anthropicRequestBody{
			Model:         req.Model,
			MaxTokens:     maxTokens,
			System:        systemBlocks,
			Messages:      messagesToAnthropicMessages(req.Messages, req.KeepReasoningHistory),
			Tools:         tools,
			Stream:        true,
			CacheControl:  &anthropicCacheControl{Type: anthropicCacheControlTypeEphemeral},
			Thinking:      thinking,
			OutputConfig:  outputCfg,
			StopSequences: append([]string(nil), req.StopSequences...),
		},
	}
}

// ToolDeclarationsToAnthropic converts provider tool declarations to Anthropic
// custom tool definitions. Nil or empty schemas fall back to an empty object.
func ToolDeclarationsToAnthropic(tools []ToolDeclaration) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = emptyObjectSchema
		}
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}

func resolveAnthropicThinking(req *GenerateRequest, opts AnthropicPlanOptions) (*anthropicThinkingConfig, *anthropicOutputConfig) {
	adaptive, enabled, disabled := anthropicThinkingCapabilities(req.Model, opts.ThinkingTypes)

	if req.SuppressThinking {
		if disabled {
			return &anthropicThinkingConfig{Type: "disabled"}, nil
		}
		if adaptive {
			return &anthropicThinkingConfig{Type: "adaptive"}, &anthropicOutputConfig{Effort: "low"}
		}
		return nil, nil
	}

	if req.Reasoning == nil || !req.Reasoning.Enabled {
		return nil, nil
	}

	effort := strings.ToLower(strings.TrimSpace(req.Reasoning.Effort))
	if effortSuppressesReasoning(effort) {
		if disabled {
			return &anthropicThinkingConfig{Type: "disabled"}, nil
		}
		if adaptive {
			return &anthropicThinkingConfig{Type: "adaptive"}, &anthropicOutputConfig{Effort: "low"}
		}
		return nil, nil
	}

	if adaptive {
		tc := &anthropicThinkingConfig{Type: "adaptive"}
		if req.IncludeThoughts {
			tc.Display = "summarized"
		}
		var oc *anthropicOutputConfig
		if effort != "" {
			if mapped := clampAnthropicEffort(effort, opts.SupportedEfforts); mapped != "" {
				oc = &anthropicOutputConfig{Effort: mapped}
			}
		}
		return tc, oc
	}

	if enabled {
		budget := req.ThinkingBudgetTokens
		if budget <= 0 {
			budget = anthropicDefaultThinkingBudget
		}
		if budget < anthropicMinThinkingBudget {
			budget = anthropicMinThinkingBudget
		}
		tc := &anthropicThinkingConfig{
			Type:         "enabled",
			BudgetTokens: budget,
		}
		if req.IncludeThoughts {
			tc.Display = "summarized"
		}
		return tc, nil
	}

	return nil, nil
}

func anthropicThinkingCapabilities(model string, discovered []string) (adaptive, enabled, disabled bool) {
	if len(discovered) > 0 {
		for _, t := range discovered {
			switch strings.ToLower(strings.TrimSpace(t)) {
			case "adaptive":
				adaptive = true
			case "enabled":
				enabled = true
			case "disabled":
				disabled = true
			}
		}
		return adaptive, enabled, disabled
	}

	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	switch {
	case strings.Contains(m, "4-5"), strings.Contains(m, "4.5"), strings.Contains(m, "claude-3"):
		return false, true, true
	case strings.Contains(m, "opus-5-5"), strings.Contains(m, "opus-5.5"),
		strings.Contains(m, "sonnet-5-5"), strings.Contains(m, "sonnet-5.5"),
		strings.Contains(m, "fable-5"), strings.Contains(m, "mythos"):
		return true, false, false
	default:
		return true, false, true
	}
}

func clampAnthropicEffort(effort string, supported []string) string {
	switch effort {
	case "minimal":
		effort = "low"
	case "low", "medium", "high", "xhigh", "max":
	default:
		return ""
	}
	if len(supported) == 0 || slices.Contains(supported, effort) {
		return effort
	}
	rank := func(e string) int {
		switch e {
		case "low":
			return 1
		case "medium":
			return 2
		case "high":
			return 3
		case "xhigh":
			return 4
		case "max":
			return 5
		default:
			return 0
		}
	}
	target := rank(effort)
	best := supported[0]
	bestDiff := 999
	for _, cand := range supported {
		cRank := rank(cand)
		diff := cRank - target
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff || (diff == bestDiff && cRank < rank(best)) {
			best = cand
			bestDiff = diff
		}
	}
	return best
}

func messagesToAnthropicMessages(messages []Message, keepReasoningHistory bool) []anthropicMessage {
	lastUserTextIdx := -1
	for i, msg := range messages {
		if msg.Role != RoleUser {
			continue
		}
		for _, p := range msg.Parts {
			if p.FunctionResponse == nil && strings.TrimSpace(p.Text) != "" {
				lastUserTextIdx = i
				break
			}
		}
	}

	legacyCounter := 0
	legacyIDs := make(map[string][]string)
	out := make([]anthropicMessage, 0, len(messages))

	appendMsg := func(role string, blocks []anthropicContentBlock) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, anthropicMessage{Role: role, Content: blocks})
	}

	for i, msg := range messages {
		if len(msg.Parts) == 0 {
			continue
		}
		switch msg.Role {
		case RoleUser:
			var results []anthropicContentBlock
			var texts []anthropicContentBlock
			for _, part := range msg.Parts {
				if part.FunctionResponse != nil {
					callID := part.FunctionResponse.CallID
					if callID == "" {
						if q := legacyIDs[part.FunctionResponse.Name]; len(q) > 0 {
							callID = q[0]
							legacyIDs[part.FunctionResponse.Name] = q[1:]
						} else {
							callID = "toolu_legacy_" + part.FunctionResponse.Name
						}
					}
					results = append(results, anthropicContentBlock{
						Type:      "tool_result",
						ToolUseID: callID,
						Content:   marshalAnthropicToolResult(part.FunctionResponse.Response),
						IsError:   anthropicToolResultIsError(part.FunctionResponse.Response),
					})
				}
				if part.Text != "" {
					texts = append(texts, anthropicContentBlock{
						Type: "text",
						Text: part.Text,
					})
				}
			}
			// tool_result blocks must precede user text blocks so they immediately
			// follow the assistant's tool_use message.
			blocks := make([]anthropicContentBlock, 0, len(results)+len(texts))
			blocks = append(blocks, results...)
			blocks = append(blocks, texts...)
			appendMsg("user", blocks)

		case RoleModel:
			inActiveTurn := lastUserTextIdx >= 0 && i > lastUserTextIdx
			keepThinking := inActiveTurn || keepReasoningHistory
			blocks := make([]anthropicContentBlock, 0, len(msg.Parts))
			for _, part := range msg.Parts {
				if part.RedactedThinking != "" {
					if keepThinking {
						blocks = append(blocks, anthropicContentBlock{
							Type: "redacted_thinking",
							Data: part.RedactedThinking,
						})
					}
					continue
				}
				if len(part.ThoughtSignature) > 0 {
					if keepThinking {
						txt := part.Reasoning
						blocks = append(blocks, anthropicContentBlock{
							Type:      "thinking",
							Thinking:  &txt,
							Signature: string(part.ThoughtSignature),
						})
					}
					// If a caller stored text/call on the same Part, keep processing below.
					if part.Text == "" && part.FunctionCall == nil {
						continue
					}
				}
				if part.Text != "" {
					blocks = append(blocks, anthropicContentBlock{
						Type: "text",
						Text: part.Text,
					})
				}
				if part.FunctionCall != nil {
					callID := part.FunctionCall.ID
					if callID == "" {
						callID = "toolu_legacy_" + part.FunctionCall.Name + "_" + strconv.Itoa(legacyCounter)
						legacyCounter++
					}
					legacyIDs[part.FunctionCall.Name] = append(legacyIDs[part.FunctionCall.Name], callID)
					input := withoutInternalCallID(part.FunctionCall.Args)
					if input == nil {
						input = map[string]any{}
					}
					blocks = append(blocks, anthropicContentBlock{
						Type:  "tool_use",
						ID:    callID,
						Name:  part.FunctionCall.Name,
						Input: input,
					})
				}
			}
			appendMsg("assistant", blocks)
		}
	}

	return out
}

func marshalAnthropicToolResult(resp map[string]any) string {
	if resp == nil {
		return "{}"
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func anthropicToolResultIsError(resp map[string]any) bool {
	if len(resp) == 0 {
		return false
	}
	if _, hasErr := resp["error"]; hasErr {
		return true
	}
	if isErr, ok := resp["isError"].(bool); ok && isErr {
		return true
	}
	return false
}

// AnthropicSseEvent models one JSON data payload from Anthropic /v1/messages SSE.
type AnthropicSseEvent struct {
	Type         string                `json:"type"`
	Index        *int                  `json:"index,omitempty"`
	Message      *anthropicSseMessage  `json:"message,omitempty"`
	ContentBlock *anthropicSseBlock    `json:"content_block,omitempty"`
	Delta        *anthropicSseDelta    `json:"delta,omitempty"`
	Usage        *anthropicSseUsage    `json:"usage,omitempty"`
	Error        *anthropicSseErrorObj `json:"error,omitempty"`
}

type anthropicSseMessage struct {
	ID         string             `json:"id"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      *anthropicSseUsage `json:"usage,omitempty"`
}

type anthropicSseBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	Signature string         `json:"signature,omitempty"`
	Data      string         `json:"data,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
}

type anthropicSseDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicSseUsage struct {
	InputTokens              *int `json:"input_tokens,omitempty"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens,omitempty"`
	OutputTokens             *int `json:"output_tokens,omitempty"`
}

type anthropicSseErrorObj struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type anthropicAccumBlock struct {
	blockType   string
	text        strings.Builder
	thinking    strings.Builder
	signature   strings.Builder
	data        string
	id          string
	name        string
	partialJSON strings.Builder
	seedInput   map[string]any
	parsedArgs  map[string]any
	stopped     bool
}

// AnthropicSseMapperState holds per-stream state while decoding Anthropic SSE events.
type AnthropicSseMapperState struct {
	blocks          map[int]*anthropicAccumBlock
	order           []int
	inputTokens     int
	cacheReadTokens int
	cacheCreateToks int
	outputTokens    int
	stopReason      string
}

// NewAnthropicSseMapperState constructs a fresh SSE mapper state.
func NewAnthropicSseMapperState() *AnthropicSseMapperState {
	return &AnthropicSseMapperState{
		blocks: make(map[int]*anthropicAccumBlock),
	}
}

func (s *AnthropicSseMapperState) applyUsage(u *anthropicSseUsage) {
	if u == nil {
		return
	}
	if u.InputTokens != nil {
		s.inputTokens = *u.InputTokens
	}
	if u.CacheReadInputTokens != nil {
		s.cacheReadTokens = *u.CacheReadInputTokens
	}
	if u.CacheCreationInputTokens != nil {
		s.cacheCreateToks = *u.CacheCreationInputTokens
	}
	if u.OutputTokens != nil {
		s.outputTokens = *u.OutputTokens
	}
}

// MapAnthropicSseEvent translates one Anthropic SSE event into StreamResponse chunks.
func MapAnthropicSseEvent(event AnthropicSseEvent, state *AnthropicSseMapperState) ([]StreamResponse, error) {
	if state == nil {
		state = NewAnthropicSseMapperState()
	}

	switch event.Type {
	case "", "ping":
		return nil, nil

	case "message_start":
		if event.Message != nil {
			state.applyUsage(event.Message.Usage)
			if event.Message.StopReason != "" {
				state.stopReason = event.Message.StopReason
			}
		}
		return nil, nil

	case "content_block_start":
		if event.ContentBlock == nil {
			return nil, nil
		}
		idx := outputIndex(event.Index)
		blk := &anthropicAccumBlock{
			blockType: event.ContentBlock.Type,
			data:      event.ContentBlock.Data,
			id:        event.ContentBlock.ID,
			name:      event.ContentBlock.Name,
			seedInput: event.ContentBlock.Input,
		}
		if event.ContentBlock.Text != "" {
			blk.text.WriteString(event.ContentBlock.Text)
		}
		if event.ContentBlock.Thinking != "" {
			blk.thinking.WriteString(event.ContentBlock.Thinking)
		}
		if event.ContentBlock.Signature != "" {
			blk.signature.WriteString(event.ContentBlock.Signature)
		}
		if _, exists := state.blocks[idx]; !exists {
			state.order = append(state.order, idx)
		}
		state.blocks[idx] = blk
		if event.ContentBlock.Text != "" {
			return []StreamResponse{{TextDelta: event.ContentBlock.Text}}, nil
		}
		return nil, nil

	case "content_block_delta":
		if event.Delta == nil {
			return nil, nil
		}
		idx := outputIndex(event.Index)
		blk := state.blocks[idx]
		if blk == nil {
			blk = &anthropicAccumBlock{}
			state.blocks[idx] = blk
			state.order = append(state.order, idx)
		}
		switch event.Delta.Type {
		case "text_delta":
			if blk.blockType == "" {
				blk.blockType = "text"
			}
			if event.Delta.Text != "" {
				blk.text.WriteString(event.Delta.Text)
				return []StreamResponse{{TextDelta: event.Delta.Text}}, nil
			}
		case "thinking_delta":
			if blk.blockType == "" {
				blk.blockType = "thinking"
			}
			if event.Delta.Thinking != "" {
				blk.thinking.WriteString(event.Delta.Thinking)
				return []StreamResponse{{ReasoningDelta: event.Delta.Thinking}}, nil
			}
		case "signature_delta":
			if event.Delta.Signature != "" {
				blk.signature.WriteString(event.Delta.Signature)
			}
		case "input_json_delta":
			if event.Delta.PartialJSON != "" {
				blk.partialJSON.WriteString(event.Delta.PartialJSON)
			}
		}
		return nil, nil

	case "content_block_stop":
		idx := outputIndex(event.Index)
		blk := state.blocks[idx]
		if blk == nil || blk.stopped {
			return nil, nil
		}
		blk.stopped = true
		if blk.blockType == "tool_use" {
			blk.parsedArgs = parseAnthropicToolArgs(blk)
			return []StreamResponse{{
				ToolCalls: []ToolCall{{
					ID:   blk.id,
					Name: blk.name,
					Args: blk.parsedArgs,
				}},
			}}, nil
		}
		return nil, nil

	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != "" {
			state.stopReason = event.Delta.StopReason
		}
		state.applyUsage(event.Usage)
		return nil, nil

	case "message_stop":
		if strings.EqualFold(strings.TrimSpace(state.stopReason), "model_context_window_exceeded") {
			return nil, fmt.Errorf("%w: model context window exceeded", ErrContextOverflow)
		}

		// Flush any unstopped tool_use block defensively.
		var extraCalls []ToolCall
		sort.Ints(state.order)
		modelParts := make([]Part, 0, len(state.order))
		for _, idx := range state.order {
			blk := state.blocks[idx]
			if blk == nil {
				continue
			}
			switch blk.blockType {
			case "thinking":
				txt := blk.thinking.String()
				sig := blk.signature.String()
				if txt != "" || sig != "" {
					modelParts = append(modelParts, Part{
						Reasoning:        txt,
						ThoughtSignature: []byte(sig),
					})
				}
			case "redacted_thinking":
				if blk.data != "" {
					modelParts = append(modelParts, Part{RedactedThinking: blk.data})
				}
			case "text":
				if txt := blk.text.String(); txt != "" {
					modelParts = append(modelParts, Part{Text: txt})
				}
			case "tool_use":
				if !blk.stopped {
					blk.stopped = true
					blk.parsedArgs = parseAnthropicToolArgs(blk)
					extraCalls = append(extraCalls, ToolCall{
						ID:   blk.id,
						Name: blk.name,
						Args: blk.parsedArgs,
					})
				}
				modelParts = append(modelParts, Part{
					FunctionCall: &ToolCall{
						ID:   blk.id,
						Name: blk.name,
						Args: blk.parsedArgs,
					},
				})
			}
		}

		var out []StreamResponse
		if len(extraCalls) > 0 {
			out = append(out, StreamResponse{ToolCalls: extraCalls})
		}

		finishReason := ""
		if anthropicFinishIsAbnormal(state.stopReason) {
			finishReason = state.stopReason
		}
		totalInput := state.inputTokens + state.cacheReadTokens + state.cacheCreateToks
		usage := &Usage{
			InputTokens:    totalInput,
			OutputTokens:   state.outputTokens,
			CachedTokens:   state.cacheReadTokens,
			CachedReported: true,
		}
		final := StreamResponse{
			Usage:        usage,
			FinishReason: finishReason,
		}
		if len(modelParts) > 0 {
			final.ModelParts = modelParts
		}
		out = append(out, final, StreamResponse{Done: true})
		return out, nil

	case "error":
		errType := ""
		msg := "Anthropic stream error"
		if event.Error != nil {
			errType = strings.TrimSpace(event.Error.Type)
			if m := strings.TrimSpace(event.Error.Message); m != "" {
				msg = m
			}
		}
		if strings.EqualFold(errType, "overloaded_error") {
			return nil, fmt.Errorf("%w: %s (retry the request)", ErrProviderOverloaded, msg)
		}
		if isOverflowMessage(strings.ToLower(msg)) {
			return nil, fmt.Errorf("%w: %s", ErrContextOverflow, msg)
		}
		if errType != "" {
			return nil, fmt.Errorf("anthropic stream error (%s): %s", errType, msg)
		}
		return nil, fmt.Errorf("anthropic stream error: %s", msg)

	default:
		// Ignore unrecognized event types per Anthropic streaming versioning policy.
		return nil, nil
	}
}

func parseAnthropicToolArgs(blk *anthropicAccumBlock) map[string]any {
	raw := strings.TrimSpace(blk.partialJSON.String())
	if raw != "" {
		return UnmarshalToolArguments(raw)
	}
	if len(blk.seedInput) > 0 {
		return blk.seedInput
	}
	return map[string]any{}
}
