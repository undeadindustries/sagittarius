package tools

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/modes"
)

var readOnlyBuiltinTools = map[string]bool{
	ReadFileToolName:        true,
	ListDirectoryToolName:   true,
	GrepToolName:            true,
	FindSymbolToolName:      true,
	activateSkillToolName:   true,
	ProjectChecksToolName:   true,
	GoogleWebSearchToolName: true,
	WebFetchToolName:        true,
}

// projectChecksFixRequested reports whether a run_project_checks call asks for
// mutating fix mode. Used to keep check-only runs read-only while denying
// file-rewriting runs in plan/ask.
func projectChecksFixRequested(args map[string]any) bool {
	fix, _, err := boolArg(args, ProjectChecksParamFix)
	return err == nil && fix
}

// InteractionModeAllow reports whether a tool call is permitted for the active
// interaction mode. workspace may be nil only when checking declaration
// visibility (no path validation).
func InteractionModeAllow(
	mode modes.Mode,
	toolName string,
	args map[string]any,
	ws *Workspace,
) (allowed bool, reason string) {
	switch mode {
	case modes.ModeAgent, modes.ModeDebug:
		return true, ""
	case modes.ModeAsk:
		return askModeAllow(canonicalToolName(toolName), args)
	case modes.ModePlan:
		return planModeAllow(canonicalToolName(toolName), args, ws)
	default:
		return true, ""
	}
}

// ToolVisibleInMode reports whether a tool should appear in provider declarations.
func ToolVisibleInMode(mode modes.Mode, toolName string) bool {
	allowed, _ := InteractionModeAllow(mode, toolName, nil, nil)
	return allowed
}

func canonicalToolName(name string) string {
	if canonical, ok := legacyAliases[name]; ok {
		return canonical
	}
	return name
}

func askModeAllow(name string, args map[string]any) (bool, string) {
	if name == ProjectChecksToolName && projectChecksFixRequested(args) {
		return false, "ask mode: run_project_checks fix mode rewrites files and is not allowed; run check-only (fix=false) instead"
	}
	if readOnlyBuiltinTools[name] {
		return true, ""
	}
	switch name {
	case WriteFileToolName:
		return false, "ask mode: writing files is not allowed; switch to agent mode to make changes"
	case ShellToolName:
		return false, "ask mode: shell commands are not allowed; use read_file, grep_search, or list_directory instead"
	default:
		if strings.HasPrefix(name, "mcp_") {
			return false, "ask mode: MCP tools are not available in read-only Q&A mode"
		}
		return false, fmt.Sprintf("ask mode: tool %q is not allowed", name)
	}
}

// grillModeAllow enforces the grill-mode read-only gate: everything askModeAllow
// permits is allowed, plus ask_user itself (the interrogation mechanism), so
// the agent can keep asking questions while writes/shell stay blocked.
func grillModeAllow(name string, args map[string]any) (bool, string) {
	if name == AskUserToolName {
		return true, ""
	}
	if allowed, reason := askModeAllow(name, args); !allowed {
		return false, "grill mode: " + strings.TrimPrefix(reason, "ask mode: ")
	}
	return true, ""
}

func planModeAllow(name string, args map[string]any, ws *Workspace) (bool, string) {
	if name == ProjectChecksToolName && projectChecksFixRequested(args) {
		return false, "plan mode: run_project_checks fix mode rewrites files and is not allowed; run check-only (fix=false) instead"
	}
	if readOnlyBuiltinTools[name] {
		return true, ""
	}
	switch name {
	case WriteFileToolName:
		if args == nil {
			return true, ""
		}
		path, err := stringArg(args, ParamFilePath)
		if err != nil {
			return false, err.Error()
		}
		if err := validatePlanWritePath(path, ws); err != nil {
			return false, err.Error()
		}
		return true, ""
	case ShellToolName:
		return false, "plan mode: shell commands are not allowed; switch to agent mode to run commands"
	default:
		if strings.HasPrefix(name, "mcp_") {
			return false, "plan mode: MCP tools are not available until you switch to agent mode"
		}
		return false, fmt.Sprintf("plan mode: tool %q is not allowed", name)
	}
}
