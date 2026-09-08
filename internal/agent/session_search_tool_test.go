package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// searchToolRunner builds a runner with a live recorder and a couple of turns
// already on disk, which is the only state search_session reads.
func searchToolRunner(t *testing.T) *Runner {
	t.Helper()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-tool-test", session.ProjectHash(dir), "main")
	rec.RecordUserMessage("deploy to gx10-01 on port 8015")
	rec.RecordModelMessage("Understood, 8015 it is.", nil)

	runner, err := NewRunner(RunnerConfig{
		Generator:       &fakeGenerator{},
		Model:           "test-model",
		WorkDir:         dir,
		Interactive:     false,
		SessionRecorder: rec,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func TestSearchSessionToolRegistered(t *testing.T) {
	t.Parallel()

	runner := searchToolRunner(t)
	tool, ok := runner.Registry().Lookup(tools.SearchSessionToolName)
	if !ok {
		t.Fatal("search_session is not registered")
	}
	if tool.RequiresConfirmation() {
		t.Error("search_session requires confirmation; it only reads the session file")
	}
}

// TestSearchSessionToolNotGatedByScratchpadSetting pins the deliberate split:
// the scratchpad costs tokens on every request and is togglable, while search
// costs one declaration and stays available.
func TestSearchSessionToolNotGatedByScratchpadSetting(t *testing.T) {
	t.Parallel()

	off := false
	runner := scratchpadToolRunner(t, &off)
	if _, ok := runner.Registry().Lookup(tools.SearchSessionToolName); !ok {
		t.Fatal("search_session was dropped along with the scratchpad; it has its own rationale")
	}
}

func TestSearchSessionToolExecute(t *testing.T) {
	t.Parallel()

	runner := searchToolRunner(t)
	tool, _ := runner.Registry().Lookup(tools.SearchSessionToolName)

	res, err := tool.Execute(context.Background(), map[string]any{
		tools.SearchSessionParamQuery: "8015",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, _ := res["count"].(int); got != 2 {
		t.Errorf("count = %v, want 2", res["count"])
	}
	matches, _ := res["matches"].(string)
	if !strings.Contains(matches, "8015") {
		t.Errorf("matches = %q, want the hit text", matches)
	}
	if !strings.Contains(matches, "turn ") {
		t.Errorf("matches = %q, want a turn label so the model can order them", matches)
	}
}

func TestSearchSessionToolRoleFilter(t *testing.T) {
	t.Parallel()

	runner := searchToolRunner(t)
	tool, _ := runner.Registry().Lookup(tools.SearchSessionToolName)

	res, err := tool.Execute(context.Background(), map[string]any{
		tools.SearchSessionParamQuery: "8015",
		tools.SearchSessionParamRole:  "user",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, _ := res["count"].(int); got != 1 {
		t.Errorf("count = %v with role=user, want 1", res["count"])
	}
}

// TestSearchSessionToolRejectsUnknownRole keeps a typo from reading as a filter
// that legitimately found nothing.
func TestSearchSessionToolRejectsUnknownRole(t *testing.T) {
	t.Parallel()

	runner := searchToolRunner(t)
	tool, _ := runner.Registry().Lookup(tools.SearchSessionToolName)

	_, err := tool.Execute(context.Background(), map[string]any{
		tools.SearchSessionParamQuery: "8015",
		tools.SearchSessionParamRole:  "assistant",
	})
	if err == nil {
		t.Fatal("Execute with role=assistant succeeded; want an error naming the valid values")
	}
	if !strings.Contains(err.Error(), "user") {
		t.Errorf("err = %v, want it to name the accepted values", err)
	}
}

// TestSearchSessionToolAcceptsFloatMaxResults covers the wire shape: JSON
// numbers decode to float64, so an int-only read would silently ignore the cap.
func TestSearchSessionToolAcceptsFloatMaxResults(t *testing.T) {
	t.Parallel()

	runner := searchToolRunner(t)
	tool, _ := runner.Registry().Lookup(tools.SearchSessionToolName)

	res, err := tool.Execute(context.Background(), map[string]any{
		tools.SearchSessionParamQuery:      "8015",
		tools.SearchSessionParamMaxResults: float64(1),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got, _ := res["returned"].(int); got != 1 {
		t.Errorf("returned = %v, want the requested 1", res["returned"])
	}
	if truncated, _ := res["truncated"].(bool); !truncated {
		t.Error("truncated is false while withholding a match; the model would think it saw everything")
	}
}

// TestSearchSessionToolWithoutRecorderErrors distinguishes "recording is off"
// from "nothing matched".
func TestSearchSessionToolWithoutRecorderErrors(t *testing.T) {
	t.Parallel()

	runner := scratchpadToolRunner(t, nil)
	tool, _ := runner.Registry().Lookup(tools.SearchSessionToolName)

	_, err := tool.Execute(context.Background(), map[string]any{
		tools.SearchSessionParamQuery: "anything",
	})
	if err == nil {
		t.Fatal("Execute with no session recorder succeeded; want an error")
	}
}
