package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

type fakeSnapshotter struct {
	captured  []string
	committed []string
}

func (f *fakeSnapshotter) CaptureWrite(absPath string) { f.captured = append(f.captured, absPath) }
func (f *fakeSnapshotter) CommitWrite(absPath, tool string) {
	f.committed = append(f.committed, absPath)
}

func TestSchedulerBoundaryBlocksOutsideWrite(t *testing.T) {
	ws := newTestWorkspace(t)
	registry := NewBuiltinRegistry(ws)
	sched := NewScheduler(registry, Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithProjectBoundary(true))

	outside := filepath.Join(filepath.Dir(ws.Root()), "escape.txt")
	resp, err := sched.Execute(context.Background(), []provider.ToolCall{{
		Name: WriteFileToolName,
		Args: map[string]any{ParamFilePath: outside, WriteFileParamContent: "x"},
	}}, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected one response, got %d", len(resp))
	}
	if _, ok := resp[0].Response["error"]; !ok {
		t.Fatalf("expected boundary error response, got %v", resp[0].Response)
	}
	if _, statErr := os.Stat(outside); !os.IsNotExist(statErr) {
		t.Fatalf("file should not have been written outside the root")
	}
}

func TestSchedulerSnapshotHookFires(t *testing.T) {
	ws := newTestWorkspace(t)
	registry := NewBuiltinRegistry(ws)
	snap := &fakeSnapshotter{}
	sched := NewScheduler(registry, Policy{Mode: ApprovalYolo}, false, nil, ws,
		WithSnapshotter(snap))

	_, err := sched.Execute(context.Background(), []provider.ToolCall{{
		Name: WriteFileToolName,
		Args: map[string]any{ParamFilePath: "note.txt", WriteFileParamContent: "hi"},
	}}, func(ui.StreamEvent) {})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(snap.captured) != 1 || len(snap.committed) != 1 {
		t.Fatalf("snapshot hooks: captured=%v committed=%v", snap.captured, snap.committed)
	}
	if !strings.HasSuffix(snap.committed[0], "note.txt") {
		t.Fatalf("unexpected committed path %q", snap.committed[0])
	}
}
