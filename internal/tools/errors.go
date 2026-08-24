package tools

// ErrorCode classifies tool execution failures for model retry logic.
type ErrorCode string

const (
	ErrCodeInvalidArgs      ErrorCode = "INVALID_ARGS"
	ErrCodeExecutionTimeout ErrorCode = "EXECUTION_TIMEOUT"
	ErrCodeSandboxDenial    ErrorCode = "SANDBOX_DENIAL"
	ErrCodeToolCrash        ErrorCode = "TOOL_CRASH"
	ErrCodeUserDenied       ErrorCode = "USER_DENIED"
	ErrCodeModeRestriction  ErrorCode = "MODE_RESTRICTION"
	ErrCodeProjectBoundary  ErrorCode = "PROJECT_BOUNDARY"
	ErrCodeUnknownTool      ErrorCode = "UNKNOWN_TOOL"
	ErrCodeHookDenied       ErrorCode = "HOOK_DENIED"
)

// ToolError is a structured tool execution failure.
type ToolError struct {
	Code    ErrorCode
	Message string
	Details map[string]any
}

func (e *ToolError) Error() string {
	if e == nil {
		return "tool error"
	}
	return e.Message
}

// ToResponse converts to a model-facing map with a backward-compatible "error" key.
func (e *ToolError) ToResponse() map[string]any {
	if e == nil {
		return map[string]any{
			"error": "unknown tool error",
			"code":  string(ErrCodeToolCrash),
		}
	}
	resp := map[string]any{
		"error": e.Message,
		"code":  string(e.Code),
	}
	if len(e.Details) > 0 {
		resp["details"] = e.Details
	}
	return resp
}
