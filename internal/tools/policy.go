package tools

// ApprovalMode controls tool confirmation policy (fork policy/types.ts subset).
type ApprovalMode string

const (
	ApprovalDefault  ApprovalMode = "default"
	ApprovalAutoEdit ApprovalMode = "autoEdit"
	ApprovalYolo     ApprovalMode = "yolo"
)

// Policy decides whether a tool invocation needs user confirmation.
type Policy struct {
	Mode ApprovalMode
}

// NeedsConfirmation returns true when the approval mode requires prompting
// before executing the given tool.
func (p Policy) NeedsConfirmation(tool Tool) bool {
	if p.Mode == ApprovalYolo {
		return false
	}
	if tool == nil {
		return false
	}
	name := tool.Name()
	if name == ShellToolName {
		return true
	}
	if p.Mode == ApprovalDefault && tool.RequiresConfirmation() {
		return true
	}
	return false
}

// HeadlessApprove returns whether a tool should run without interactive confirmation.
func (p Policy) HeadlessApprove(tool Tool) bool {
	return !p.NeedsConfirmation(tool)
}
