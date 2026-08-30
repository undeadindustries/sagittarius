package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/prompt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// parentAgentID labels the top-level agent in the shared file-state registry.
// Subagents use their own session id.
const parentAgentID = "parent"

// registerSubagentTools registers the two subagent classes independently. Each
// is off by default and neither inherits the other's switch: research children
// read, coding children write, and a user who wants only the first must not
// silently get the second.
func registerSubagentTools(r *Runner, registry *tools.Registry, settings *config.Settings) {
	if registry == nil || r == nil {
		return
	}
	// A subagent may not launch further subagents. The tools also enforce this
	// at call time; leaving them unregistered keeps them out of the child's
	// declarations so it never tries.
	if r.isSubagent() {
		return
	}
	if config.ResearchSubagentsEnabled(settings, nil) {
		registry.Register(newTaskTool(r))
	}
	if config.CodingSubagentsEnabled(settings, nil) {
		registry.Register(newCodeTaskTool(r))
	}
}

// isSubagent reports whether this runner is itself a child. A child with no
// recorder still counts: it carries a non-parent agent id.
func (r *Runner) isSubagent() bool {
	if r == nil {
		return false
	}
	if r.sessionRecorder != nil && r.sessionRecorder.Kind() == sessionKindSubagent {
		return true
	}
	return r.agentID != "" && r.agentID != parentAgentID
}

type codeTaskTool struct {
	runner *Runner
}

func newCodeTaskTool(r *Runner) tools.Tool { return &codeTaskTool{runner: r} }

func (t *codeTaskTool) Name() string { return tools.CodeTaskToolName }

func (t *codeTaskTool) Description() string {
	return "Launch a coding subagent that makes one bounded change and reports back. " +
		"You must declare write_paths: the subagent can modify those paths and nothing else. " +
		"Issue several calls in one turn to work on disjoint parts of the codebase in parallel; " +
		"leases that overlap are rejected before any subagent starts. " +
		"The subagent's shell is read-only, so it verifies with run_project_checks and cannot run tests — run those yourself after it returns."
}

func (t *codeTaskTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				tools.TaskParamDescription: map[string]any{
					"type":        "string",
					"description": "A short, user-friendly title for the change (e.g. 'Add retry to the HTTP client').",
				},
				tools.TaskParamPrompt: map[string]any{
					"type":        "string",
					"description": "Complete instructions for the subagent. It cannot ask you questions, so state the goal, the constraints, and what done looks like.",
				},
				tools.CodeTaskParamWritePaths: map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Workspace-relative paths this subagent may modify. Supports globs and '**' for any depth; a trailing slash means the whole directory (e.g. 'internal/http/**', 'docs/api.md'). Every other path is denied.",
				},
			},
			"required": []string{
				tools.TaskParamDescription,
				tools.TaskParamPrompt,
				tools.CodeTaskParamWritePaths,
			},
		},
	}
}

// RequiresConfirmation gates the whole child behind one prompt showing its
// lease, instead of letting a headless child raise a confirmation per write.
func (t *codeTaskTool) RequiresConfirmation() bool { return true }

func (t *codeTaskTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteStream(ctx, args, func(string) {})
}

func (t *codeTaskTool) ExecuteStream(ctx context.Context, args map[string]any, sink tools.ToolOutputSink) (map[string]any, error) {
	spec, err := parseCodeTaskArgs(args)
	if err != nil {
		return nil, err
	}
	if err := t.runner.checkSubagentDepth(); err != nil {
		return nil, err
	}

	startedAt := time.Now()
	child, err := t.runner.newSubagent(ctx, subagentSpec{
		description: spec.description,
		mode:        modes.ModeAgent,
		class:       config.SubagentCoding,
		lease:       &spec.lease,
		charter:     prompt.CodingSubagentCharter(spec.lease.Patterns),
		snapshotter: t.runner.snap,
		approval:    ApprovalYolo,
	})
	if err != nil {
		return nil, err
	}

	text, err := child.run(ctx, spec.prompt, sink)
	if err != nil {
		return nil, err
	}

	written := child.runner.writtenPaths()
	result := map[string]any{
		"status":        "completed",
		"result":        text,
		"lease":         spec.lease.Patterns,
		"files_written": written,
	}
	if notice := t.parentRereadNotice(startedAt); notice != "" {
		result["result"] = text + "\n\n" + notice
		result["stale_warnings"] = notice
	}
	return result, nil
}

// parentRereadNotice tells the parent which files it had already read were
// changed while the child ran. Without it the parent edits from a stale view of
// a file the child just rewrote.
func (t *codeTaskTool) parentRereadNotice(since time.Time) string {
	changed := t.runner.fileState.WritesSince(t.runner.agentID, since)
	if len(changed) == 0 {
		return ""
	}
	rel := make([]string, 0, len(changed))
	for _, abs := range changed {
		if r, err := t.runner.workspace.RelativePath(abs); err == nil {
			rel = append(rel, r)
			continue
		}
		rel = append(rel, abs)
	}
	sort.Strings(rel)
	return fmt.Sprintf("[NOTE: files you previously read were modified — re-read before editing: %s]",
		strings.Join(rel, ", "))
}

type codeTaskSpec struct {
	description string
	prompt      string
	lease       tools.WriteLease
}

func parseCodeTaskArgs(args map[string]any) (codeTaskSpec, error) {
	promptText, err := stringArg(args, tools.TaskParamPrompt)
	if err != nil {
		return codeTaskSpec{}, err
	}
	lease, err := tools.ParseWriteLease(args[tools.CodeTaskParamWritePaths])
	if err != nil {
		return codeTaskSpec{}, err
	}
	desc, _ := stringArg(args, tools.TaskParamDescription)
	if desc == "" {
		desc = "Coding subagent"
	}
	return codeTaskSpec{description: desc, prompt: promptText, lease: lease}, nil
}
