package agent

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestHandleBangEmptyUsage(t *testing.T) {
	t.Parallel()
	app := NewApp(AppConfig{})
	events, err := app.HandleInput(testContext(t), "!")
	if err != nil {
		t.Fatalf("HandleInput: %v", err)
	}
	got := collectEvents(t, events)
	if len(got) < 2 {
		t.Fatalf("events = %d, want usage + done", len(got))
	}
	if got[0].Type != ui.StreamInfo || !strings.Contains(got[0].Text, "Usage:") {
		t.Fatalf("first event = %+v, want StreamInfo usage", got[0])
	}
	if got[len(got)-1].Type != ui.StreamDone {
		t.Fatalf("last event = %v, want StreamDone", got[len(got)-1].Type)
	}
}

func TestHandleBangEchoDoesNotTouchHistory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ws, err := tools.NewWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := NewCatalog(CatalogConfig{Workspace: ws})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     dir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	app := NewApp(AppConfig{Runner: runner, Runtime: &Runtime{Catalog: cat}})

	events, err := app.HandleInput(testContext(t), "!echo bang-ok")
	if err != nil {
		t.Fatalf("HandleInput: %v", err)
	}
	got := collectEvents(t, events)

	var sawStart, sawResult, sawDone bool
	for _, ev := range got {
		switch ev.Type {
		case ui.StreamToolStart:
			sawStart = true
			if ev.ToolName != tools.ShellToolName {
				t.Errorf("ToolName = %q", ev.ToolName)
			}
			if !strings.HasPrefix(ev.ToolCallID, ui.BangCallIDPrefix) {
				t.Errorf("ToolCallID = %q, want %s prefix", ev.ToolCallID, ui.BangCallIDPrefix)
			}
			if ev.Text != "echo bang-ok" {
				t.Errorf("start text = %q", ev.Text)
			}
		case ui.StreamToolResult:
			sawResult = true
			if !strings.Contains(ev.Text, "bang-ok") {
				t.Errorf("result text = %q, want bang-ok", ev.Text)
			}
			if ev.IsError {
				t.Errorf("result marked error: %q", ev.Text)
			}
		case ui.StreamDone:
			sawDone = true
		}
	}
	if !sawStart || !sawResult || !sawDone {
		t.Fatalf("events missing start/result/done: %+v", got)
	}
	if len(runner.History()) != 0 {
		t.Fatalf("history = %d messages, want 0", len(runner.History()))
	}
	gen.mu.Lock()
	calls := gen.call
	gen.mu.Unlock()
	if calls != 0 {
		t.Fatalf("generator calls = %d, want 0", calls)
	}
}

func TestHandleBangMissingShell(t *testing.T) {
	t.Parallel()
	app := NewApp(AppConfig{})
	events, err := app.HandleInput(testContext(t), "!echo hi")
	if err != nil {
		t.Fatalf("HandleInput: %v", err)
	}
	got := collectEvents(t, events)
	var sawErr bool
	for _, ev := range got {
		if ev.Type == ui.StreamError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("events = %+v, want StreamError", got)
	}
}
