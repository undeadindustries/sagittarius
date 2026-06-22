package provider

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
	ToolCalls []ToolCall
	Usage     *Usage
	Done      bool
	Error     error
	// ModelParts, when non-nil, carries the complete set of model content parts
	// for the turn (including Gemini thoughtSignature metadata). It is set once
	// on the final chunk so the runner can store the model message verbatim
	// rather than reconstructing it from flat text + tool calls. OpenAI-family
	// generators leave this nil and the runner falls back to text/tool-call
	// reconstruction.
	ModelParts []Part
}
