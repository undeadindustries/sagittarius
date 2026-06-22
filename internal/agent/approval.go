package agent

import (
	"fmt"
	"strings"
)

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

// ParseApprovalMode converts a user-facing approval-mode name to an ApprovalMode.
// It accepts the fork alias "auto_edit" for autoEdit. The fork's "plan" approval
// mode is intentionally rejected: Sagittarius planning is an interaction mode
// (--mode plan), not an approval policy (see AD-022).
func ParseApprovalMode(name string) (ApprovalMode, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "default":
		return ApprovalDefault, nil
	case "autoedit", "auto_edit":
		return ApprovalAutoEdit, nil
	case "yolo":
		return ApprovalYolo, nil
	case "plan":
		return "", fmt.Errorf("approval-mode %q is a fork concept; use --mode plan for Sagittarius interaction-mode planning (AD-022)", name)
	default:
		return "", fmt.Errorf("unknown approval mode %q (want default, autoEdit, or yolo)", name)
	}
}
