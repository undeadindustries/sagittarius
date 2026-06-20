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

// StreamResponse is one chunk emitted from GenerateContentStream.
// Multiple fields may be set in a single chunk (e.g. text and tool calls).
type StreamResponse struct {
	TextDelta string
	ToolCalls []ToolCall
	Done      bool
	Error     error
}
