package provider

import "strings"

// Role identifies who produced a message in a conversation turn.
type Role string

const (
	RoleUser  Role = "user"
	RoleModel Role = "model"
)

// Part is a single content fragment within a Message.
type Part struct {
	Text             string
	FunctionCall     *ToolCall
	FunctionResponse *FunctionResponse
	// ThoughtSignature is Gemini-only metadata: an opaque, encrypted handle for
	// the model's reasoning attached to a model part (text or functionCall).
	// Gemini 3 requires it to be replayed verbatim on model functionCall parts
	// within the active tool-calling turn, otherwise the API returns a 400.
	// Other providers ignore this field.
	ThoughtSignature []byte
}

// FunctionResponse carries the result of a tool invocation back to the model.
type FunctionResponse struct {
	Name     string
	CallID   string
	Response map[string]any
}

// Message is one turn in a multi-turn conversation.
type Message struct {
	Role  Role
	Parts []Part
}

// ToolCall is a model-initiated function invocation.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolDeclaration describes a callable function exposed to the model.
type ToolDeclaration struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// GenerateRequest is the provider-neutral input for content generation.
type GenerateRequest struct {
	Model             string
	SystemInstruction string
	Messages          []Message
	Tools             []ToolDeclaration
	Temperature       *float64
	MaxOutputTokens   *int32
	StopSequences     []string
	// IncludeThoughts requests that the provider stream readable thought text
	// alongside the answer. For the Gemini native adapter this sets
	// ThinkingConfig.IncludeThoughts; for OpenAI-family adapters the field is
	// ignored (they expose reasoning through their own wire fields).
	IncludeThoughts bool
	// Reasoning carries the resolved reasoning ask for this round (see
	// config.ResolveReasoningRequest), or nil when nothing should be sent
	// (current default behavior). Each adapter interprets it in terms of its
	// own wire mechanism: Gemini maps it to ThinkingConfig.ThinkingBudget/
	// ThinkingLevel, OpenAI Responses maps it to reasoning.effort, and
	// openai-chat maps it to a unified reasoning:{enabled,effort} object.
	Reasoning *ReasoningRequest
	// ThinkingBudgetTokens caps the tokens the model may spend reasoning in
	// this round. Zero means unset. Adapters advertise it to servers that
	// enforce a budget natively (llama.cpp reasoning_budget_tokens, Gemini
	// ThinkingBudget); servers that do not recognize it ignore the field.
	ThinkingBudgetTokens int
	// SuppressThinking asks the provider to skip reasoning entirely for this
	// round. It is set only when a previous round was cut short by the
	// client-side thinking budget, so the retry must answer rather than think
	// again. Adapters send every suppression key they know, because no single
	// key works across llama.cpp, OpenRouter and Qwen-on-vLLM.
	SuppressThinking bool
}

// ThinkingBudgetMessage is the wrap-up text a server injects at the end of a
// spent thinking budget, immediately before the thinking close tag, so the
// model sees why its reasoning ended rather than finding it cut mid-thought.
//
// It is not cosmetic. llama.cpp measured Qwen3.5 9B HumanEval at 89% when the
// budget ended with a message against 79% for a bare truncation — worse than
// disabling thinking entirely. The trailing newline is deliberate: without it,
// reasoning has been observed to run past the close tag.
const ThinkingBudgetMessage = "\n\n... reasoning budget exceeded. I have enough to answer now.\n"

// maxThinkingBudgetTokens bounds a configured budget before it is narrowed to
// the int32 the Gemini SDK takes, so a nonsensical value cannot wrap negative.
const maxThinkingBudgetTokens = 1 << 30

// ReasoningRequest describes the resolved reasoning ask for one round.
// Effort pins a level (minimal/low/medium/high/xhigh/none); empty means
// "no pinned level". Enabled, when Effort is empty, means "turn reasoning
// on and let the provider apply its own default effort" -- the closest
// thing to adaptive that a fixed-effort-only wire format offers.
type ReasoningRequest struct {
	Effort  string
	Enabled bool
}

// ProducesReasoning reports whether this ask will make the model reason at all,
// and therefore whether asking for readable reasoning text is meaningful. An
// effort of none/off disables thinking, so there would be nothing to stream back
// and requesting a summary alongside it would contradict the same request. A nil
// receiver reports false.
func (r *ReasoningRequest) ProducesReasoning() bool {
	return r != nil && r.Enabled && !effortSuppressesReasoning(r.Effort)
}

// effortSuppressesReasoning reports whether an effort level turns thinking off
// rather than dialing it down.
func effortSuppressesReasoning(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "none", "off":
		return true
	}
	return false
}

// Usage holds provider-reported token counts and optional cost for one request.
// CostKnown is true only when the provider explicitly reported a cost (currently
// only OpenRouter). A zero-cost value with CostKnown=true means the model was
// free, not that cost was unavailable.
type Usage struct {
	InputTokens  int
	OutputTokens int
	CostUSD      float64
	CostKnown    bool
}

// StreamResponse is one chunk emitted from GenerateContentStream.
// Multiple fields may be set in a single chunk (e.g. text and tool calls).
// Usage, when non-nil, carries the provider-reported token counts and optional
// cost. It is set once on the final chunk (alongside or just before Done=true).
type StreamResponse struct {
	TextDelta string
	// ReasoningDelta carries incremental model reasoning ("thinking") text,
	// kept separate from TextDelta so the UI can show it in a dedicated,
	// optional thinking view rather than mixing it into the answer. Populated by:
	//   - Gemini native: Part.Thought text when IncludeThoughts is set
	//   - OpenAI-chat / OpenRouter: reasoning or reasoning_content SSE fields
	//   - OpenAI Responses: response.reasoning_summary_text.delta
	ReasoningDelta string
	ToolCalls      []ToolCall
	Usage          *Usage
	Done           bool
	Error          error
	// ModelParts, when non-nil, carries the complete set of model content parts
	// for the turn (including Gemini thoughtSignature metadata). It is set once
	// on the final chunk so the runner can store the model message verbatim
	// rather than reconstructing it from flat text + tool calls. OpenAI-family
	// generators leave this nil and the runner falls back to text/tool-call
	// reconstruction.
	ModelParts []Part
}
