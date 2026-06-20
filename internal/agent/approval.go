package agent

// ApprovalMode controls how tool invocations are approved before execution.
// Phase 07 supports default only; yolo and plan arrive in Phase 09/15.
type ApprovalMode string

const (
	// ApprovalDefault requires user confirmation for destructive tools (stub in Phase 07).
	ApprovalDefault ApprovalMode = "default"
)
