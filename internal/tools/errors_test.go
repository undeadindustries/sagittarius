package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestErrorResponseKeepsErrorKeyAndCode(t *testing.T) {
	t.Parallel()
	resp := errorResponse(provider.ToolCall{Name: "t", ID: "1"}, ErrCodeUnknownTool, "unknown tool \"t\"")
	if resp.Response["error"] != `unknown tool "t"` {
		t.Fatalf("error = %v", resp.Response["error"])
	}
	if resp.Response["code"] != string(ErrCodeUnknownTool) {
		t.Fatalf("code = %v", resp.Response["code"])
	}
}

func TestSchedulerErrorCodes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ws, err := NewWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewBuiltinRegistry(ws)

	tests := []struct {
		name string
		call provider.ToolCall
		opts []SchedulerOption
		mode func() modes.Mode
		want ErrorCode
	}{
		{
			name: "unknown tool",
			call: provider.ToolCall{Name: "not_a_tool", Args: map[string]any{}},
			want: ErrCodeUnknownTool,
		},
		{
			name: "project boundary",
			call: provider.ToolCall{Name: WriteFileToolName, Args: map[string]any{
				ParamFilePath:         filepath.Join(filepath.Dir(root), "escape.txt"),
				WriteFileParamContent: "x\n",
			}},
			opts: []SchedulerOption{WithProjectBoundary(true)},
			want: ErrCodeProjectBoundary,
		},
		{
			name: "mode restriction",
			call: provider.ToolCall{Name: WriteFileToolName, Args: map[string]any{
				ParamFilePath:         "x.txt",
				WriteFileParamContent: "x\n",
			}},
			mode: func() modes.Mode { return modes.ModeAsk },
			want: ErrCodeModeRestriction,
		},
		{
			name: "invalid write args",
			call: provider.ToolCall{Name: WriteFileToolName, Args: map[string]any{
				ParamFilePath: "x.txt",
			}},
			want: ErrCodeInvalidArgs,
		},
		{
			name: "hook deny",
			call: provider.ToolCall{Name: WriteFileToolName, Args: map[string]any{
				ParamFilePath:         "x.txt",
				WriteFileParamContent: "x\n",
			}},
			opts: []SchedulerOption{WithHooks(func(context.Context, string, map[string]any) (map[string]any, bool, string, error) {
				return nil, true, "blocked", nil
			}, nil)},
			want: ErrCodeHookDenied,
		},
		{
			name: "user denied",
			call: provider.ToolCall{Name: WriteFileToolName, Args: map[string]any{
				ParamFilePath:         "x.txt",
				WriteFileParamContent: "x\n",
			}},
			want: ErrCodeUserDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := Policy{Mode: ApprovalYolo}
			if tt.want == ErrCodeUserDenied {
				policy = Policy{Mode: ApprovalDefault}
			}
			opts := append([]SchedulerOption{}, tt.opts...)
			sched := NewScheduler(registry, policy, false, tt.mode, ws, opts...)
			resps, err := sched.Execute(context.Background(), []provider.ToolCall{tt.call}, func(ui.StreamEvent) {})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(resps) != 1 {
				t.Fatalf("got %d responses", len(resps))
			}
			got, _ := resps[0].Response["code"].(string)
			if got != string(tt.want) {
				t.Fatalf("code = %q, want %q (resp=%v)", got, tt.want, resps[0].Response)
			}
			if _, ok := resps[0].Response["error"].(string); !ok {
				t.Fatalf("missing backward-compat error key: %v", resps[0].Response)
			}
			if tt.want == ErrCodeProjectBoundary {
				if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(statErr) {
					t.Fatal("boundary write escaped")
				}
			}
		})
	}
}
