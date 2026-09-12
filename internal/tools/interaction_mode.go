package tools

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/modes"
)

type ReadOnlyPolicy int

const (
	PolicyNone ReadOnlyPolicy = iota
	PolicyStrict
	PolicyInspect
	// PolicyShellInspect filters run_shell_command exactly as PolicyInspect
	// does but leaves file mutations alone: a coding subagent exists to write,
	// and its writes are bounded by a path lease instead. Shell cannot be
	// leased (sed -i, a > redirect, a script that moves files), so it stays
	// read-only.
	PolicyShellInspect
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
	TaskToolName:            true,
	ScriptToolName:          true,
	// The working-memory tools touch nothing outside the session: the
	// scratchpad is runner state plus a session-JSONL line, and search_session
	// only reads the file this session is already writing. save_memory is
	// deliberately absent from this map (it writes MEMORY.md on disk), which
	// is the line these two stay on the safe side of.
	UpdateScratchpadToolName: true,
	SearchSessionToolName:    true,
	WaitUntilToolName:        true,
}

// projectChecksFixRequested reports whether a run_project_checks call asks for
// mutating fix mode. Used to keep check-only runs read-only while denying
// file-rewriting runs in plan/ask.
func projectChecksFixRequested(args map[string]any) bool {
	fix, _, err := boolArg(args, ProjectChecksParamFix)
	return err == nil && fix
}

// ReadOnlyHinter is implemented by tools that can vouch for themselves as
// non-mutating. Built-ins are covered by readOnlyBuiltinTools, so in practice
// this exists for MCP tools, whose behavior is only knowable from the server's
// own annotations plus what the user has vouched for in settings.
type ReadOnlyHinter interface {
	ReadOnlyHint() bool
}

// toolIsReadOnly reports whether tool declares itself safe in read-only modes.
// A nil tool answers false: callers that cannot resolve the tool (declaration
// filtering before registration, hook-rewrite revalidation) must not be able to
// admit an MCP tool by accident.
func toolIsReadOnly(tool Tool) bool {
	hinter, ok := tool.(ReadOnlyHinter)
	return ok && hinter.ReadOnlyHint()
}

// mcpDenyHint names the way to admit an MCP tool to read-only modes. A deny
// with no remedy is what taught models to invent bad advice (see AD-107).
const mcpDenyHint = " — add it to the server's readOnlyTools in settings, " +
	"or trust the server if it declares readOnlyHint, then run /mcp reload"

// InteractionModeAllow reports whether a tool call is permitted for the active
// interaction mode. workspace may be nil only when checking declaration
// visibility (no path validation). Prefer InteractionModeAllowTool where the
// resolved tool is at hand: without it an MCP tool cannot present its
// read-only annotation and is denied in ask and plan mode.
func InteractionModeAllow(
	mode modes.Mode,
	toolName string,
	args map[string]any,
	ws *Workspace,
) (allowed bool, reason string) {
	return InteractionModeAllowTool(mode, nil, toolName, args, ws)
}

// InteractionModeAllowTool is InteractionModeAllow with the resolved tool, so a
// read-only MCP tool can be admitted to a read-only mode. tool may be nil.
func InteractionModeAllowTool(
	mode modes.Mode,
	tool Tool,
	toolName string,
	args map[string]any,
	ws *Workspace,
) (allowed bool, reason string) {
	switch mode {
	case modes.ModeAgent, modes.ModeDebug:
		return true, ""
	case modes.ModeAsk:
		return askModeAllow(canonicalToolName(toolName), args, tool)
	case modes.ModePlan:
		return planModeAllow(canonicalToolName(toolName), args, ws, tool)
	default:
		return true, ""
	}
}

// ToolVisibleInMode reports whether a tool should appear in provider declarations.
func ToolVisibleInMode(mode modes.Mode, toolName string) bool {
	allowed, _ := InteractionModeAllow(mode, toolName, nil, nil)
	return allowed
}

// ToolValueVisibleInMode is ToolVisibleInMode for a resolved tool, so a
// read-only MCP tool is declared to the model in ask and plan mode rather than
// being offered and then denied on use.
func ToolValueVisibleInMode(mode modes.Mode, tool Tool) bool {
	allowed, _ := InteractionModeAllowTool(mode, tool, tool.Name(), nil, nil)
	return allowed
}

func canonicalToolName(name string) string {
	if canonical, ok := legacyAliases[name]; ok {
		return canonical
	}
	return name
}

func askModeAllow(name string, args map[string]any, tool Tool) (bool, string) {
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
		return false, "ask mode: shell commands are not allowed; use read_file, grep_search, list_directory, find_symbol, google_web_search, or web_fetch instead"
	default:
		if strings.HasPrefix(name, "mcp_") {
			if toolIsReadOnly(tool) {
				return true, ""
			}
			return false, "ask mode: MCP tool " + name + " is not marked read-only" + mcpDenyHint
		}
		return false, fmt.Sprintf("ask mode: tool %q is not allowed", name)
	}
}

// grillModeAllow enforces the grill-mode read-only gate: everything askModeAllow
// permits is allowed, plus ask_user itself (the interrogation mechanism), so
// the agent can keep asking questions while writes/shell stay blocked.
func grillModeAllow(name string, args map[string]any, tool Tool) (bool, string) {
	if name == AskUserToolName {
		return true, ""
	}
	if allowed, reason := askModeAllow(name, args, tool); !allowed {
		return false, "grill mode: " + strings.TrimPrefix(reason, "ask mode: ")
	}
	return true, ""
}

// readOnlyExit names the way out of the inspection posture. Every deny that
// the posture causes carries it: without a remediation hint the model has no
// way to tell the user what to do, and has been observed inventing worse
// advice (restart the process, switch modes) that does not work.
const readOnlyExit = " — run /readonly off to allow changes"

func inspectModeAllow(name string, args map[string]any, tool Tool) (bool, string) {
	if name == AskUserToolName {
		return true, ""
	}
	if name == ProjectChecksToolName && projectChecksFixRequested(args) {
		return false, "inspect mode: run_project_checks fix mode rewrites files and is not allowed; run check-only (fix=false) instead" + readOnlyExit
	}
	if readOnlyBuiltinTools[name] {
		return true, ""
	}

	switch name {
	case WriteFileToolName, EditToolName:
		return false, "inspect mode: modifying files is not allowed; session is in read-only inspection mode" + readOnlyExit
	case SaveMemoryToolName:
		return false, "inspect mode: modifying memory is not allowed in inspection mode" + readOnlyExit
	case ShellToolName:
		cmd, err := stringArg(args, ShellParamCommand)
		if err != nil {
			return false, "invalid command"
		}
		verdict, reason := ClassifyShellReadOnly(cmd)
		if verdict == VerdictMutating {
			return false, "inspect mode: mutating shell command denied (" + reason + ")" + readOnlyExit
		}
		// VerdictReadOnly and VerdictUnknown pass the hard-deny gate.
		// Unknown will trigger an interactive confirmation downstream in requestApproval.
		return true, ""
	default:
		if strings.HasPrefix(name, "mcp_") {
			if toolIsReadOnly(tool) {
				return true, ""
			}
			return false, "inspect mode: MCP tool " + name + " is not marked read-only" + mcpDenyHint + readOnlyExit
		}
		return false, fmt.Sprintf("inspect mode: tool %q is not allowed", name) + readOnlyExit
	}
}

// shellVerdictEscalates reports whether a policy sends an unclassifiable shell
// command to confirmation rather than running it. A headless child fails that
// confirmation closed, which is the intended outcome for a command neither the
// classifier nor a human has vouched for.
func shellVerdictEscalates(p ReadOnlyPolicy) bool {
	return p == PolicyInspect || p == PolicyShellInspect
}

// shellInspectAllow is the PolicyShellInspect gate. Only run_shell_command is
// filtered; everything else is left to the write-lease gate and the ordinary
// interaction-mode rules.
func shellInspectAllow(name string, args map[string]any) (bool, string) {
	if name != ShellToolName {
		return true, ""
	}
	cmd, err := stringArg(args, ShellParamCommand)
	if err != nil {
		return false, "invalid command"
	}
	if verdict, reason := ClassifyShellReadOnly(cmd); verdict == VerdictMutating {
		return false, "subagent shell is read-only: " + reason +
			"; use write_file or edit inside your lease, and run_project_checks to verify"
	}
	// VerdictUnknown passes here and is escalated to a confirmation the child,
	// being headless, will fail closed on.
	return true, ""
}

func planModeAllow(name string, args map[string]any, ws *Workspace, tool Tool) (bool, string) {
	if name == ProjectChecksToolName && projectChecksFixRequested(args) {
		return false, "plan mode: run_project_checks fix mode rewrites files and is not allowed; run check-only (fix=false) instead"
	}
	if readOnlyBuiltinTools[name] {
		return true, ""
	}
	switch name {
	case WriteFileToolName, EditToolName:
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
			if toolIsReadOnly(tool) {
				return true, ""
			}
			return false, "plan mode: MCP tool " + name + " is not marked read-only" + mcpDenyHint
		}
		return false, fmt.Sprintf("plan mode: tool %q is not allowed", name)
	}
}
