package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

const maxScriptOperations = 32

type scriptOp struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

type scriptTool struct {
	registry *Registry
}

func newScriptTool(registry *Registry) Tool {
	return &scriptTool{registry: registry}
}

func (t *scriptTool) Name() string { return ScriptToolName }

func (t *scriptTool) RequiresConfirmation() bool { return false }

func (t *scriptTool) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{
		Name: ScriptToolName,
		Description: "Execute a batch of read-only operations in one call. " +
			"The script runs locally without LLM round-trips between operations. " +
			"Use this to collapse multi-step research (grep, read, list, find_symbol) into a single turn. " +
			"Mutating tools, shell, nested run_script, and confirmation-gated tools are rejected.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				ScriptParamScript: map[string]any{
					"type": "string",
					"description": `JSON script of operations:
{"operations":[{"tool":"grep_search","args":{"pattern":"foo","dir_path":"src/"}}]}
A bare JSON array of the same objects is also accepted.`,
				},
			},
			"required": []string{ScriptParamScript},
		},
	}
}

func (t *scriptTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	return t.ExecuteBatch(ctx, args, t.localRunner())
}

func (t *scriptTool) ExecuteBatch(ctx context.Context, args map[string]any, run NestedToolRunner) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t == nil || t.registry == nil {
		return nil, &ToolError{Code: ErrCodeToolCrash, Message: "run_script has no tool registry"}
	}
	if run == nil {
		run = t.localRunner()
	}
	script, err := stringArg(args, ScriptParamScript)
	if err != nil {
		return nil, &ToolError{Code: ErrCodeInvalidArgs, Message: err.Error()}
	}
	ops, err := parseScript(script)
	if err != nil {
		return nil, &ToolError{Code: ErrCodeInvalidArgs, Message: err.Error()}
	}
	if len(ops) == 0 {
		return nil, &ToolError{Code: ErrCodeInvalidArgs, Message: "script must contain at least one operation"}
	}
	if len(ops) > maxScriptOperations {
		return nil, &ToolError{
			Code:    ErrCodeInvalidArgs,
			Message: fmt.Sprintf("script has %d operations; maximum is %d", len(ops), maxScriptOperations),
		}
	}

	results := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, t.executeOp(ctx, op, run))
	}
	return map[string]any{"results": results}, nil
}

func (t *scriptTool) localRunner() NestedToolRunner {
	return func(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
		tool, ok := t.registry.Lookup(name)
		if !ok {
			return nil, &ToolError{Code: ErrCodeUnknownTool, Message: fmt.Sprintf("unknown tool %q", name)}
		}
		return tool.Execute(ctx, args)
	}
}

func (t *scriptTool) executeOp(ctx context.Context, op scriptOp, run NestedToolRunner) map[string]any {
	name := canonicalToolName(op.Tool)
	tool, ok := t.registry.Lookup(name)
	if !ok {
		return map[string]any{
			"tool":  op.Tool,
			"error": fmt.Sprintf("unknown tool %q", op.Tool),
			"code":  string(ErrCodeUnknownTool),
		}
	}
	if err := scriptAllow(name, tool, op.Args); err != nil {
		return map[string]any{
			"tool":  name,
			"error": err.Error(),
			"code":  string(ErrCodeModeRestriction),
		}
	}
	result, err := run(ctx, name, op.Args)
	if err != nil {
		var te *ToolError
		if errors.As(err, &te) {
			out := te.ToResponse()
			out["tool"] = name
			return out
		}
		return map[string]any{
			"tool":  name,
			"error": err.Error(),
			"code":  string(ErrCodeToolCrash),
		}
	}
	return map[string]any{"tool": name, "output": result}
}

func scriptAllow(name string, tool Tool, args map[string]any) error {
	if name == ScriptToolName || name == TaskToolName {
		return fmt.Errorf("tool %q is not allowed in scripts", name)
	}
	if !readOnlyBuiltinTools[name] {
		return fmt.Errorf("tool %q is not allowed in scripts (read-only only)", name)
	}
	if name == ProjectChecksToolName && projectChecksFixRequested(args) {
		return fmt.Errorf("run_project_checks fix mode is not allowed in scripts")
	}
	if tool.RequiresConfirmation() {
		return fmt.Errorf("tool %q requires confirmation and cannot run inside run_script", name)
	}
	return nil
}

func parseScript(script string) ([]scriptOp, error) {
	var payload struct {
		Operations []scriptOp `json:"operations"`
	}
	if err := json.Unmarshal([]byte(script), &payload); err == nil && payload.Operations != nil {
		return payload.Operations, nil
	}
	var ops []scriptOp
	if err := json.Unmarshal([]byte(script), &ops); err != nil {
		return nil, fmt.Errorf("invalid script JSON: %w", err)
	}
	return ops, nil
}
