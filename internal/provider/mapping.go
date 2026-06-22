package provider

import (
	"encoding/json"
	"strconv"
	"strings"

	"google.golang.org/genai"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// MessagesToGenaiContents converts provider messages to Gemini Content values.
func MessagesToGenaiContents(messages []Message) []*genai.Content {
	out := make([]*genai.Content, 0, len(messages))
	for _, msg := range messages {
		if content := messageToGenaiContent(msg); content != nil {
			out = append(out, content)
		}
	}
	return out
}

func messageToGenaiContent(msg Message) *genai.Content {
	parts := partsToGenai(msg.Parts)
	if len(parts) == 0 {
		return nil
	}
	return &genai.Content{
		Role:  string(msg.Role),
		Parts: parts,
	}
}

func partsToGenai(parts []Part) []*genai.Part {
	out := make([]*genai.Part, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != "":
			out = append(out, genai.NewPartFromText(part.Text))
		case part.FunctionCall != nil:
			out = append(out, genai.NewPartFromFunctionCall(
				part.FunctionCall.Name,
				part.FunctionCall.Args,
			))
		case part.FunctionResponse != nil:
			out = append(out, genai.NewPartFromFunctionResponse(
				part.FunctionResponse.Name,
				part.FunctionResponse.Response,
			))
		}
	}
	return out
}

// ToolDeclarationsToGenai converts provider tool declarations to Gemini tools.
func ToolDeclarationsToGenai(tools []ToolDeclaration) []*genai.Tool {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		decls = append(decls, toolDeclarationToGenai(tool))
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}
}

func toolDeclarationToGenai(tool ToolDeclaration) *genai.FunctionDeclaration {
	decl := &genai.FunctionDeclaration{
		Name:        tool.Name,
		Description: tool.Description,
	}
	if len(tool.Parameters) > 0 {
		decl.ParametersJsonSchema = tool.Parameters
	}
	return decl
}

// GenaiPartsToParts converts Gemini parts back to provider parts.
func GenaiPartsToParts(parts []*genai.Part) []Part {
	out := make([]Part, 0, len(parts))
	for _, part := range parts {
		if part == nil {
			continue
		}
		switch {
		case part.Text != "":
			out = append(out, Part{Text: part.Text})
		case part.FunctionCall != nil:
			out = append(out, Part{FunctionCall: functionCallFromGenai(part.FunctionCall)})
		case part.FunctionResponse != nil:
			out = append(out, Part{
				FunctionResponse: &FunctionResponse{
					Name:     part.FunctionResponse.Name,
					Response: part.FunctionResponse.Response,
				},
			})
		}
	}
	return out
}

// ToolCallsFromGenaiResponse extracts tool calls from a stream chunk.
func ToolCallsFromGenaiResponse(resp *genai.GenerateContentResponse) []ToolCall {
	if resp == nil {
		return nil
	}
	calls := resp.FunctionCalls()
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		if tc := functionCallFromGenai(call); tc != nil {
			out = append(out, *tc)
		}
	}
	return out
}

func functionCallFromGenai(call *genai.FunctionCall) *ToolCall {
	if call == nil {
		return nil
	}
	args := call.Args
	if args == nil {
		args = map[string]any{}
	}
	return &ToolCall{
		ID:   call.ID,
		Name: call.Name,
		Args: args,
	}
}

// BuildGenerateContentConfig assembles a Gemini GenerateContentConfig from a request.
func BuildGenerateContentConfig(req *GenerateRequest) *genai.GenerateContentConfig {
	if req == nil {
		return &genai.GenerateContentConfig{}
	}

	cfg := &genai.GenerateContentConfig{}
	if req.SystemInstruction != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.SystemInstruction, genai.RoleUser)
	}
	if req.Temperature != nil {
		temp := float32(*req.Temperature)
		cfg.Temperature = &temp
	}
	if req.MaxOutputTokens != nil {
		cfg.MaxOutputTokens = *req.MaxOutputTokens
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = append([]string(nil), req.StopSequences...)
	}
	if tools := ToolDeclarationsToGenai(req.Tools); len(tools) > 0 {
		cfg.Tools = tools
	}
	return cfg
}

var emptyObjectSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{},
}

// ToolDeclarationsToOpenAI converts provider tool declarations to OpenAI tools.
func ToolDeclarationsToOpenAI(tools []ToolDeclaration) []openAITool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		params := tool.Parameters
		if params == nil {
			params = emptyObjectSchema
		}
		out = append(out, openAITool{
			Type: "function",
			Function: openAIToolSchema{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

// MessagesToOpenAIMessages converts provider messages to OpenAI chat messages.
func MessagesToOpenAIMessages(messages []Message, modelID string) []OpenAIMessage {
	out := make([]OpenAIMessage, 0, len(messages))
	isMistral := IsMistralFamilyModel(modelID)
	toolCallCounter := 0

	for _, msg := range messages {
		mapped := messageToOpenAIMessages(msg, &toolCallCounter)
		for _, m := range mapped {
			if isMistral &&
				m.Role == OpenAIRoleUser &&
				len(out) > 0 &&
				out[len(out)-1].Role == OpenAIRoleTool {
				bridge := mistralToolUserBridgeContent
				out = append(out, OpenAIMessage{Role: OpenAIRoleAssistant, Content: &bridge})
			}
			out = append(out, m)
		}
	}

	afterOrphan := patchOrphanedToolCallsForMistral(out, modelID)
	return patchToolUserTransitionForMistral(afterOrphan, modelID)
}

func messageToOpenAIMessages(msg Message, counter *int) []OpenAIMessage {
	if len(msg.Parts) == 0 {
		return nil
	}

	var textParts []string
	var toolCalls []openAIToolCall
	var out []OpenAIMessage

	for _, part := range msg.Parts {
		switch {
		case part.FunctionResponse != nil:
			rawID := "call_" + part.FunctionResponse.Name + "_" + strconv.Itoa(*counter)
			*counter++
			content, _ := json.Marshal(part.FunctionResponse.Response)
			s := string(content)
			out = append(out, OpenAIMessage{
				Role:       OpenAIRoleTool,
				ToolCallID: MistralSafeToolCallID(rawID),
				Content:    &s,
			})
		case part.FunctionCall != nil:
			rawID := "call_" + part.FunctionCall.Name + "_" + strconv.Itoa(*counter)
			*counter++
			args, _ := json.Marshal(part.FunctionCall.Args)
			toolCalls = append(toolCalls, openAIToolCall{
				ID:   MistralSafeToolCallID(rawID),
				Type: "function",
				Function: openAIFunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(args),
				},
			})
		case part.Text != "":
			textParts = append(textParts, part.Text)
		}
	}

	if len(toolCalls) > 0 {
		var content *string
		if joined := strings.Join(textParts, ""); joined != "" {
			content = &joined
		}
		out = append(out, OpenAIMessage{
			Role:      OpenAIRoleAssistant,
			Content:   content,
			ToolCalls: toolCalls,
		})
		return out
	}

	if len(textParts) > 0 {
		joined := strings.Join(textParts, "")
		role := OpenAIRoleUser
		if msg.Role == RoleModel {
			role = OpenAIRoleAssistant
		}
		out = append(out, OpenAIMessage{Role: role, Content: &joined})
	}
	return out
}

// BuildOpenAIChatRequest assembles an OpenAI chat completions request body.
// defaultTemperature supplies the generator's effective temperature when the
// request does not carry its own; either may be nil to send none.
func BuildOpenAIChatRequest(req *GenerateRequest, model string, parseMode config.ToolCallParsingMode, defaultTemperature *float64) openAIChatRequest {
	_ = parseMode
	body := openAIChatRequest{
		Model:         model,
		Messages:      MessagesToOpenAIMessages(req.Messages, model),
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}
	if req.SystemInstruction != "" {
		sys := req.SystemInstruction
		body.Messages = append([]OpenAIMessage{{Role: OpenAIRoleSystem, Content: &sys}}, body.Messages...)
	}
	if tools := ToolDeclarationsToOpenAI(req.Tools); len(tools) > 0 {
		body.Tools = tools
	}
	if req.Temperature != nil {
		body.Temperature = req.Temperature
	} else if defaultTemperature != nil {
		body.Temperature = defaultTemperature
	}
	if req.MaxOutputTokens != nil {
		body.MaxTokens = req.MaxOutputTokens
	}
	if len(req.StopSequences) > 0 {
		body.Stop = append([]string(nil), req.StopSequences...)
	}
	return body
}
