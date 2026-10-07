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
		var gp *genai.Part
		switch {
		case part.Text != "":
			gp = genai.NewPartFromText(part.Text)
		case part.FunctionCall != nil:
			gp = genai.NewPartFromFunctionCall(
				part.FunctionCall.Name,
				part.FunctionCall.Args,
			)
			if part.FunctionCall.ID != "" && gp.FunctionCall != nil {
				gp.FunctionCall.ID = part.FunctionCall.ID
			}
		case part.FunctionResponse != nil:
			gp = genai.NewPartFromFunctionResponse(
				part.FunctionResponse.Name,
				part.FunctionResponse.Response,
			)
			if part.FunctionResponse.CallID != "" && gp.FunctionResponse != nil {
				gp.FunctionResponse.ID = part.FunctionResponse.CallID
			}
		default:
			continue
		}
		// Replay the Gemini thought signature verbatim. Required on model
		// functionCall parts within the active tool-calling turn (see AD on
		// thought signatures); harmless on text parts.
		if len(part.ThoughtSignature) > 0 {
			gp.ThoughtSignature = part.ThoughtSignature
		}
		out = append(out, gp)
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
		var p Part
		switch {
		case part.Text != "":
			p = Part{Text: part.Text}
		case part.FunctionCall != nil:
			p = Part{FunctionCall: functionCallFromGenai(part.FunctionCall)}
		case part.FunctionResponse != nil:
			p = Part{
				FunctionResponse: &FunctionResponse{
					Name:     part.FunctionResponse.Name,
					Response: part.FunctionResponse.Response,
				},
			}
		default:
			continue
		}
		if len(part.ThoughtSignature) > 0 {
			p.ThoughtSignature = part.ThoughtSignature
		}
		out = append(out, p)
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
	if req.Temperature != nil && !isGeminiLevelModel(req.Model) {
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
	if req.IncludeThoughts || (req.Reasoning != nil && req.Reasoning.Enabled) || req.SuppressThinking {
		tc := &genai.ThinkingConfig{}
		if req.IncludeThoughts && !req.SuppressThinking {
			tc.IncludeThoughts = true
		}
		applyReasoningToThinkingConfig(tc, req.Reasoning, req.Model)
		applyThinkingBudgetToThinkingConfig(tc, req)
		cfg.ThinkingConfig = tc
	}
	return cfg
}

// applyThinkingBudgetToThinkingConfig overlays an explicit token budget onto
// Gemini's ThinkingConfig, after applyReasoningToThinkingConfig has set the
// effort-derived default.
//
// Gemini 2.5 takes a raw token count, so a configured budget maps straight
// onto it — the native enforcement AD-077 deliberately declined to guess from
// an effort string, but which a user-supplied number states outright. Gemini 3+
// has no numeric budget (it exposes ThinkingLevel instead), so a budget is
// never sent to level-based Gemini models to avoid 400 INVALID_ARGUMENT.
func applyThinkingBudgetToThinkingConfig(tc *genai.ThinkingConfig, req *GenerateRequest) {
	if req.SuppressThinking {
		if isGeminiLevelModel(req.Model) {
			// Gemini 3+ cannot disable thinking entirely; map to the lowest
			// supported level for the model instead of sending ThinkingBudget=0.
			tc.ThinkingBudget = nil
			tc.ThinkingLevel = lowestGeminiThinkingLevel(req.Model)
			return
		}
		off := int32(0)
		tc.ThinkingBudget = &off
		tc.ThinkingLevel = ""
		return
	}
	if req.ThinkingBudgetTokens > 0 && !isGeminiLevelModel(req.Model) {
		budget := int32(min(req.ThinkingBudgetTokens, maxThinkingBudgetTokens))
		tc.ThinkingBudget = &budget
	}
}

// applyReasoningToThinkingConfig translates a resolved ReasoningRequest into
// Gemini's ThinkingConfig fields.
//
// For Gemini 3+ (level-based models):
//   - Empty effort (adaptive/dynamic default): omit both ThinkingBudget and
//     ThinkingLevel, allowing the model to use its native default.
//   - "none"/"off": Gemini 3+ cannot disable thinking; set the model's lowest
//     supported level.
//   - Pinned levels ("minimal", "low", "medium", "high"): clamp to the model's
//     supported levels and set ThinkingLevel.
//   - ThinkingBudget is never set under any circumstance.
//
// For Gemini 2.5 (budget-based models):
//   - Empty effort maps to dynamic (ThinkingBudget=-1).
//   - "none"/"off" disables thinking (ThinkingBudget=0).
//   - Pinned levels fall back to dynamic (ThinkingBudget=-1).
func applyReasoningToThinkingConfig(tc *genai.ThinkingConfig, reasoning *ReasoningRequest, model string) {
	if reasoning == nil || !reasoning.Enabled {
		return
	}
	isLevelModel := isGeminiLevelModel(model)
	effort := strings.ToLower(strings.TrimSpace(reasoning.Effort))

	if isLevelModel {
		tc.ThinkingBudget = nil
		switch effort {
		case "":
			// Adaptive default: leave ThinkingLevel unset so the model uses its default.
			tc.ThinkingLevel = ""
		case "none", "off":
			tc.ThinkingLevel = lowestGeminiThinkingLevel(model)
		case "minimal", "low", "medium", "high":
			tc.ThinkingLevel = clampGeminiThinkingLevel(model, effort)
		default:
			if level, ok := geminiThinkingLevel(effort); ok {
				tc.ThinkingLevel = level
			}
		}
		return
	}

	switch effort {
	case "":
		budget := int32(-1)
		tc.ThinkingBudget = &budget
	case "none", "off":
		budget := int32(0)
		tc.ThinkingBudget = &budget
	case "minimal", "low", "medium", "high":
		budget := int32(-1)
		tc.ThinkingBudget = &budget
	}
}

// isGeminiLevelModel reports whether model belongs to the Gemini 3+ family,
// which uses ThinkingLevel and rejects ThinkingBudget and custom sampling parameters.
// This matches any Gemini model that is not an older 1.x or 2.x generation.
func isGeminiLevelModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	if !strings.Contains(m, "gemini-") {
		return false
	}
	// Older budget-based or legacy families
	if strings.Contains(m, "gemini-2.") || strings.Contains(m, "gemini-1.") || strings.Contains(m, "gemini-1-") || strings.Contains(m, "gemini-2-") {
		return false
	}
	return true
}

// geminiSupportedLevels returns the supported thinking levels for known Gemini 3+ models.
// The table mirrors "Controlling thinking" at
// https://ai.google.dev/gemini-api/docs/thinking; update it when Google adds a row.
// Gemini 2.5 is deliberately absent: although the docs table lists levels for it,
// the deprecation notice only names the Gemini 3 series, so 2.5 keeps its token
// budget (see isGeminiLevelModel) until Google says otherwise.
// Returns nil if the model is unknown (allowing pass-through).
func geminiSupportedLevels(model string) []genai.ThinkingLevel {
	m := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}

	switch {
	// gemini-3.8-flash, gemini-3.7-flash: low, medium, high (no minimal)
	case strings.Contains(m, "gemini-3.8-flash"), strings.Contains(m, "gemini-3-8-flash"),
		strings.Contains(m, "gemini-3.7-flash"), strings.Contains(m, "gemini-3-7-flash"):
		return []genai.ThinkingLevel{genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh}
	// gemini-3-pro-preview: low, high
	case strings.Contains(m, "gemini-3-pro-preview"):
		return []genai.ThinkingLevel{genai.ThinkingLevelLow, genai.ThinkingLevelHigh}
	// gemini-3.1-pro-preview: low, medium, high
	case strings.Contains(m, "gemini-3.1-pro"), strings.Contains(m, "gemini-3-1-pro"):
		return []genai.ThinkingLevel{genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh}
	// gemini-3.1-flash-lite-image: minimal, high
	case strings.Contains(m, "gemini-3.1-flash-lite-image"), strings.Contains(m, "gemini-3-1-flash-lite-image"):
		return []genai.ThinkingLevel{genai.ThinkingLevelMinimal, genai.ThinkingLevelHigh}
	// Models supporting minimal, low, medium, high:
	// gemini-3.6-flash, gemini-3.5-flash, gemini-3.5-flash-lite, gemini-3-flash-preview, etc.
	case strings.Contains(m, "gemini-3.6-flash"), strings.Contains(m, "gemini-3-6-flash"),
		strings.Contains(m, "gemini-3.5-flash"), strings.Contains(m, "gemini-3-5-flash"),
		strings.Contains(m, "gemini-3-flash"):
		return []genai.ThinkingLevel{genai.ThinkingLevelMinimal, genai.ThinkingLevelLow, genai.ThinkingLevelMedium, genai.ThinkingLevelHigh}
	default:
		return nil
	}
}

// lowestGeminiThinkingLevel returns the lowest valid thinking level for a Gemini model.
// If the model is unknown, it defaults to ThinkingLevelLow for safety.
func lowestGeminiThinkingLevel(model string) genai.ThinkingLevel {
	supported := geminiSupportedLevels(model)
	if len(supported) > 0 {
		return supported[0]
	}
	return genai.ThinkingLevelLow
}

// clampGeminiThinkingLevel clamps an effort string to the nearest supported thinking level
// for the given model. If the model is unknown, it passes the effort through.
func clampGeminiThinkingLevel(model, effort string) genai.ThinkingLevel {
	reqLevel, ok := geminiThinkingLevel(effort)
	if !ok {
		return ""
	}
	supported := geminiSupportedLevels(model)
	if len(supported) == 0 {
		return reqLevel
	}

	for _, lvl := range supported {
		if lvl == reqLevel {
			return reqLevel
		}
	}

	// Not directly supported: map to the nearest level.
	// Ordered rank: minimal (1), low (2), medium (3), high (4).
	levelRank := func(l genai.ThinkingLevel) int {
		switch l {
		case genai.ThinkingLevelMinimal:
			return 1
		case genai.ThinkingLevelLow:
			return 2
		case genai.ThinkingLevelMedium:
			return 3
		case genai.ThinkingLevelHigh:
			return 4
		default:
			return 0
		}
	}

	reqRank := levelRank(reqLevel)
	best := supported[0]
	bestDiff := 999
	for _, cand := range supported {
		cRank := levelRank(cand)
		diff := cRank - reqRank
		if diff < 0 {
			diff = -diff
		}
		// Tie-break: if diff is equal, choose the lower rank to avoid over-thinking
		if diff < bestDiff || (diff == bestDiff && cRank < levelRank(best)) {
			best = cand
			bestDiff = diff
		}
	}
	return best
}

// geminiThinkingLevel maps a Sagittarius effort string to the genai
// ThinkingLevel enum.
func geminiThinkingLevel(effort string) (genai.ThinkingLevel, bool) {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal":
		return genai.ThinkingLevelMinimal, true
	case "low":
		return genai.ThinkingLevelLow, true
	case "medium":
		return genai.ThinkingLevelMedium, true
	case "high":
		return genai.ThinkingLevelHigh, true
	default:
		return "", false
	}
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

	legacyCounter := 0
	legacyIDs := make(map[string][]string)

	for _, msg := range messages {
		mapped := messageToOpenAIMessages(msg, &legacyCounter, legacyIDs)
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

	// Enforce tool-call/result integrity for every provider before applying the
	// Mistral-specific role-bridging patches.
	repaired := repairToolCallIntegrity(out)
	return patchToolUserTransitionForMistral(repaired, modelID)
}

func messageToOpenAIMessages(msg Message, legacyCounter *int, legacyIDs map[string][]string) []OpenAIMessage {
	if len(msg.Parts) == 0 {
		return nil
	}

	var textParts []string
	var toolCalls []openAIToolCall
	var out []OpenAIMessage

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
			content, _ := json.Marshal(part.FunctionResponse.Response)
			s := string(content)
			out = append(out, OpenAIMessage{
				Role:       OpenAIRoleTool,
				ToolCallID: rawID,
				Content:    &s,
			})
		case part.FunctionCall != nil:
			rawID := part.FunctionCall.ID
			if rawID == "" {
				rawID = "call_" + part.FunctionCall.Name + "_" + strconv.Itoa(*legacyCounter)
				*legacyCounter++
			}
			// Always register the id so that responses loaded without a CallID
			// (e.g. old session JSONL saved before the CallID fix) can fall back
			// to the correct id rather than producing a bogus "call_<name>" string.
			legacyIDs[part.FunctionCall.Name] = append(legacyIDs[part.FunctionCall.Name], rawID)
			toolCalls = append(toolCalls, openAIToolCall{
				ID:   rawID,
				Type: "function",
				Function: openAIFunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: marshalToolCallArgs(part.FunctionCall.Args),
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
	if req.Reasoning != nil && req.Reasoning.Enabled {
		body.Reasoning = &openAIReasoning{Effort: req.Reasoning.Effort, Enabled: true}
	}
	applyThinkingBudgetToChatRequest(&body, req)
	return body
}

// applyThinkingBudgetToChatRequest sets the thinking-budget and
// thinking-suppression fields on an openai-chat body.
//
// Suppression wins over a budget: it is only set on a retry after the
// client-side budget already cut a round short, and asking that retry to think
// "a bit less" instead of "not at all" would just spend the budget again. No
// single key suppresses thinking across backends, so all three are sent —
// llama.cpp honors a zero budget, OpenRouter honors reasoning.enabled, and
// Qwen-family templates on vLLM/SGLang honor enable_thinking. Backends ignore
// the keys they do not know.
func applyThinkingBudgetToChatRequest(body *openAIChatRequest, req *GenerateRequest) {
	if req.SuppressThinking {
		zero := 0
		off := false
		body.ReasoningBudgetTokens = &zero
		body.Reasoning = &openAIReasoning{Enabled: false}
		body.ChatTemplateKwargs = &chatTemplateKwargs{EnableThinking: &off}
		return
	}
	if req.ThinkingBudgetTokens > 0 {
		budget := req.ThinkingBudgetTokens
		body.ReasoningBudgetTokens = &budget
		body.ReasoningBudgetMessage = ThinkingBudgetMessage
	}
}
