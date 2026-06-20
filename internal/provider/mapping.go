package provider

import (
	"google.golang.org/genai"
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
