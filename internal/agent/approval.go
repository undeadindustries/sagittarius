package agent

// ApprovalMode controls how tool invocations are approved before execution.
type ApprovalMode string

const (
	// ApprovalDefault requires user confirmation for destructive tools (write_file) and shell.
	ApprovalDefault ApprovalMode = "default"
	// ApprovalAutoEdit auto-approves file edits; still confirms shell commands.
	ApprovalAutoEdit ApprovalMode = "autoEdit"
	// ApprovalYolo runs all tools without confirmation (path validation still applies).
	ApprovalYolo ApprovalMode = "yolo"
)
