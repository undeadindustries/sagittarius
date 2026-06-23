package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// responsesInputItem is one element of the Responses API input array.
type responsesInputItem struct {
	Type      string                  `json:"type"`
	Role      string                  `json:"role,omitempty"`
	Content   []responsesInputContent `json:"content,omitempty"`
	CallID    string                  `json:"call_id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Arguments string                  `json:"arguments,omitempty"`
	Output    string                  `json:"output,omitempty"`
}

type responsesInputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// responsesTool is a flat function tool declaration for /v1/responses.
type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type responsesRequestBody struct {
	Model              string               `json:"model"`
	Input              []responsesInputItem `json:"input"`
	Stream             bool                 `json:"stream"`
	Instructions       string               `json:"instructions,omitempty"`
	Tools              []responsesTool      `json:"tools,omitempty"`
	Temperature        *float64             `json:"temperature,omitempty"`
	Reasoning          *responsesReasoning  `json:"reasoning,omitempty"`
	PreviousResponseID string               `json:"previous_response_id,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort"`
}

// ResponsesRequestPlan is the translated request payload for /v1/responses.
type ResponsesRequestPlan struct {
	Input        []responsesInputItem
	Tools        []responsesTool
	Instructions string
}

// ResponsesSseEvent is a parsed SSE payload from /v1/responses.
type ResponsesSseEvent struct {
	Type        string                  `json:"type"`
	Delta       string                  `json:"delta,omitempty"`
	OutputIndex *int                    `json:"output_index,omitempty"`
	Item        *responsesSseItem       `json:"item,omitempty"`
	Response    *responsesSseResponse   `json:"response,omitempty"`
	Error       *responsesSseErrorField `json:"error,omitempty"`
}

type responsesSseItem struct {
	ID        string `json:"id,omitempty"`
	Type      string `json:"type,omitempty"`
	Role      string `json:"role,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	CallID    string `json:"call_id,omitempty"`
}

type responsesSseResponse struct {
	ID     string                  `json:"id,omitempty"`
	Status string                  `json:"status,omitempty"`
	Usage  *responsesSseUsage      `json:"usage,omitempty"`
	Error  *responsesSseErrorField `json:"error,omitempty"`
}

type responsesSseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

type responsesSseErrorField struct {
	Message string `json:"message,omitempty"`
	Type    string `json:"type,omitempty"`
}

type pendingResponsesFunctionCall struct {
	callID    string
	name      string
	arguments strings.Builder
}

// ResponsesSseMapperState tracks SSE accumulation across one stream.
type ResponsesSseMapperState struct {
	PendingFunctionCalls map[int]*pendingResponsesFunctionCall
	ResponseID           string
	Completed            bool
}

// NewResponsesSseMapperState returns fresh SSE mapper state.
func NewResponsesSseMapperState() *ResponsesSseMapperState {
	return &ResponsesSseMapperState{
		PendingFunctionCalls: map[int]*pendingResponsesFunctionCall{},
	}
}

// ToolDeclarationsToResponses converts provider tools to Responses API shape.
func ToolDeclarationsToResponses(tools []ToolDeclaration) []responsesTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Parameters
		if params == nil {
			params = emptyObjectSchema
		}
		out = append(out, responsesTool{
			Type:        "function",
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  params,
		})
	}
	return out
}

// BuildResponsesRequestPlan translates a GenerateRequest to Responses input.
func BuildResponsesRequestPlan(req *GenerateRequest, toolsEnabled bool) ResponsesRequestPlan {
	if req == nil {
		return ResponsesRequestPlan{}
	}
	plan := ResponsesRequestPlan{
		Instructions: strings.TrimSpace(req.SystemInstruction),
	}

	legacyCounter := 0
	legacyIDs := make(map[string][]string)

	for _, msg := range req.Messages {
		plan.Input = append(plan.Input, messageToResponsesInput(msg, &legacyCounter, legacyIDs)...)
	}
	if toolsEnabled && len(req.Tools) > 0 {
		plan.Tools = ToolDeclarationsToResponses(req.Tools)
	}
	return plan
}

func messageToResponsesInput(msg Message, legacyCounter *int, legacyIDs map[string][]string) []responsesInputItem {
	if len(msg.Parts) == 0 {
		return nil
	}

	role := "user"
	textType := "input_text"
	if msg.Role == RoleModel {
		role = "assistant"
		textType = "output_text"
	}

	var items []responsesInputItem
	var textParts []string

	for _, part := range msg.Parts {
		switch {
		case part.FunctionResponse != nil:
			rawID := part.FunctionResponse.CallID
			if rawID == "" {
				if q := legacyIDs[part.FunctionResponse.Name]; len(q) > 0 {
					rawID = q[0]
					legacyIDs[part.FunctionResponse.Name] = q[1:]
				} else {
					rawID = "call_" + part.FunctionResponse.Name
				}
			}
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: safeResponsesCallID(rawID),
				Output: marshalFunctionResponse(part.FunctionResponse.Response),
			})
		case part.FunctionCall != nil:
			rawID := part.FunctionCall.ID
			if rawID == "" {
				rawID = "call_" + part.FunctionCall.Name + "_" + strconv.Itoa(*legacyCounter)
				*legacyCounter++
				legacyIDs[part.FunctionCall.Name] = append(legacyIDs[part.FunctionCall.Name], rawID)
			}
			args, _ := json.Marshal(part.FunctionCall.Args)
			items = append(items, responsesInputItem{
				Type:      "function_call",
				CallID:    safeResponsesCallID(rawID),
				Name:      part.FunctionCall.Name,
				Arguments: string(args),
			})
		case part.Text != "":
			textParts = append(textParts, part.Text)
		}
	}

	if len(textParts) > 0 {
		items = append(items, responsesInputItem{
			Type: "message",
			Role: role,
			Content: []responsesInputContent{{
				Type: textType,
				Text: strings.Join(textParts, ""),
			}},
		})
	}
	return items
}

func marshalFunctionResponse(resp map[string]any) string {
	if resp == nil {
		return "{}"
	}
	b, err := json.Marshal(resp)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func safeResponsesCallID(rawID string) string {
	if rawID == "" {
		return "call_unknown"
	}
	var b strings.Builder
	for _, r := range rawID {
		if r >= 0x20 && r <= 0x7E {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return "call_unknown"
	}
	if len(s) > 256 {
		return s[:256]
	}
	return s
}

// TrimInputForChaining returns the tail of input for previous_response_id chaining.
func TrimInputForChaining(input []responsesInputItem) []responsesInputItem {
	if len(input) == 0 {
		return input
	}
	lastUser := -1
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Type == "message" && input[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser >= 0 {
		return input[lastUser:]
	}
	out := make([]responsesInputItem, 0, len(input))
	for _, item := range input {
		if item.Type == "function_call_output" {
			out = append(out, item)
		}
	}
	return out
}

// MapResponsesSseEvent converts one Responses SSE event to stream chunks.
func MapResponsesSseEvent(event ResponsesSseEvent, state *ResponsesSseMapperState) ([]StreamResponse, error) {
	if state == nil {
		state = NewResponsesSseMapperState()
	}
	if event.Type == "" {
		return nil, nil
	}

	switch event.Type {
	case "response.created", "response.in_progress", "response.content_part.added",
		"response.content_part.done", "response.output_text.done",
		"response.reasoning_summary_text.done", "response.reasoning_text.done",
		"response.function_call_arguments.done":
		return nil, nil

	case "response.output_item.added":
		if event.Item != nil && event.Item.Type == "function_call" {
			idx := outputIndex(event.OutputIndex)
			state.PendingFunctionCalls[idx] = &pendingResponsesFunctionCall{
				callID: firstNonEmpty(event.Item.CallID, event.Item.ID, fmt.Sprintf("call_%d", idx)),
				name:   event.Item.Name,
			}
			if event.Item.Arguments != "" {
				state.PendingFunctionCalls[idx].arguments.WriteString(event.Item.Arguments)
			}
		}
		return nil, nil

	case "response.output_text.delta":
		if event.Delta != "" {
			return []StreamResponse{{TextDelta: event.Delta}}, nil
		}
		return nil, nil

	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		if event.Delta != "" {
			return []StreamResponse{{TextDelta: event.Delta}}, nil
		}
		return nil, nil

	case "response.function_call_arguments.delta":
		idx := outputIndex(event.OutputIndex)
		existing := state.PendingFunctionCalls[idx]
		if existing == nil {
			existing = &pendingResponsesFunctionCall{
				callID: firstNonEmpty(itemCallID(event.Item), fmt.Sprintf("call_%d", idx)),
				name:   itemName(event.Item),
			}
			state.PendingFunctionCalls[idx] = existing
		}
		existing.arguments.WriteString(event.Delta)
		return nil, nil

	case "response.output_item.done":
		if event.Item != nil && event.Item.Type == "function_call" {
			idx := outputIndex(event.OutputIndex)
			accum := state.PendingFunctionCalls[idx]
			if accum != nil {
				if event.Item.Arguments != "" {
					accum.arguments.Reset()
					accum.arguments.WriteString(event.Item.Arguments)
				}
				if event.Item.Name != "" {
					accum.name = event.Item.Name
				}
				if event.Item.CallID != "" {
					accum.callID = event.Item.CallID
				}
				calls := []ToolCall{pendingToToolCall(accum)}
				delete(state.PendingFunctionCalls, idx)
				return []StreamResponse{{ToolCalls: calls}}, nil
			}
		}
		return nil, nil

	case "response.completed":
		state.Completed = true
		if event.Response != nil && event.Response.ID != "" {
			state.ResponseID = event.Response.ID
		}
		var out []StreamResponse
		if len(state.PendingFunctionCalls) > 0 {
			out = append(out, StreamResponse{ToolCalls: flushPendingToolCalls(state.PendingFunctionCalls)})
			state.PendingFunctionCalls = map[int]*pendingResponsesFunctionCall{}
		}
		// Emit provider-reported token counts from the completed response.
		if event.Response != nil && event.Response.Usage != nil {
			out = append(out, StreamResponse{Usage: &Usage{
				InputTokens:  event.Response.Usage.InputTokens,
				OutputTokens: event.Response.Usage.OutputTokens,
			}})
		}
		out = append(out, StreamResponse{Done: true})
		return out, nil

	case "response.failed", "response.incomplete", "error", "response.error":
		msg := "OpenAI Responses stream reported an error"
		if event.Response != nil && event.Response.Error != nil && event.Response.Error.Message != "" {
			msg = event.Response.Error.Message
		} else if event.Error != nil && event.Error.Message != "" {
			msg = event.Error.Message
		}
		return nil, fmt.Errorf("openai responses error: %s", msg)

	default:
		return nil, nil
	}
}

func outputIndex(idx *int) int {
	if idx == nil {
		return 0
	}
	return *idx
}

func itemCallID(item *responsesSseItem) string {
	if item == nil {
		return ""
	}
	return item.CallID
}

func itemName(item *responsesSseItem) string {
	if item == nil {
		return ""
	}
	return item.Name
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func pendingToToolCall(p *pendingResponsesFunctionCall) ToolCall {
	args := map[string]any{}
	raw := p.arguments.String()
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &args)
	}
	if args == nil {
		args = map[string]any{}
	}
	return ToolCall{
		ID:   p.callID,
		Name: p.name,
		Args: args,
	}
}

func flushPendingToolCalls(pending map[int]*pendingResponsesFunctionCall) []ToolCall {
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
		out = append(out, pendingToToolCall(pending[idx]))
	}
	return out
}
