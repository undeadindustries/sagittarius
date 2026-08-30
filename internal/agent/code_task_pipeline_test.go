package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/storage"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// routedGenerator answers by the first user message rather than by call order,
// so several children generating at once stay deterministic. fakeGenerator
// cannot do this: it pops a shared queue, and concurrent subagents would take
// each other's turns.
type routedGenerator struct {
	mu sync.Mutex
	// scripts maps a routing key to the successive turns for that agent.
	scripts map[string][][]provider.StreamResponse
	seen    map[string]int
}

func newRoutedGenerator(scripts map[string][][]provider.StreamResponse) *routedGenerator {
	return &routedGenerator{scripts: scripts, seen: map[string]int{}}
}

func (g *routedGenerator) GenerateContentStream(ctx context.Context, req *provider.GenerateRequest) (<-chan provider.StreamResponse, error) {
	key := g.route(req)

	g.mu.Lock()
	turn := g.seen[key]
	g.seen[key] = turn + 1
	var responses []provider.StreamResponse
	if script, ok := g.scripts[key]; ok && turn < len(script) {
		responses = append([]provider.StreamResponse(nil), script[turn]...)
	}
	g.mu.Unlock()

	if responses == nil {
		responses = []provider.StreamResponse{{TextDelta: "done", Done: true}}
	}

	ch := make(chan provider.StreamResponse)
	go func() {
		defer close(ch)
		for _, resp := range responses {
			select {
			case <-ctx.Done():
				return
			case ch <- resp:
			}
		}
	}()
	return ch, nil
}

// route picks the script key from the first user text, which is the prompt the
// agent was launched with and never changes across its turns.
func (g *routedGenerator) route(req *provider.GenerateRequest) string {
	for _, msg := range req.Messages {
		if msg.Role != provider.RoleUser {
			continue
		}
		for _, p := range msg.Parts {
			if text := strings.TrimSpace(p.Text); text != "" {
				for key := range g.scripts {
					if strings.Contains(text, key) {
						return key
					}
				}
				return text
			}
		}
	}
	return ""
}

func (g *routedGenerator) turns(key string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen[key]
}

func codeTaskCall(id, desc, prompt string, paths ...string) provider.ToolCall {
	claims := make([]any, len(paths))
	for i, p := range paths {
		claims[i] = p
	}
	return provider.ToolCall{
		ID:   id,
		Name: tools.CodeTaskToolName,
		Args: map[string]any{
			tools.TaskParamDescription:    desc,
			tools.TaskParamPrompt:         prompt,
			tools.CodeTaskParamWritePaths: claims,
		},
	}
}

func writeCall(id, path, content string) provider.ToolCall {
	return provider.ToolCall{
		ID:   id,
		Name: tools.WriteFileToolName,
		Args: map[string]any{
			tools.ParamFilePath:         path,
			tools.WriteFileParamContent: content,
		},
	}
}

// TestCodingSubagentPipeline drives three coding subagents concurrently through
// the real scheduler, tool registry, and snapshot manager. It is the end-to-end
// check that a lease actually confines a child, that one child's failure does
// not discard a sibling's committed work, and that parallel commits leave a
// readable snapshot index.
func TestCodingSubagentPipeline(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "alpha/a.txt", "alpha-before")
	seedFile(t, root, "beta/b.txt", "beta-before")

	const sessionID = "pipeline"
	snapMgr, err := snapshot.NewManager(root, sessionID, snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}

	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{
					codeTaskCall("c1", "alpha work", "TASK-ALPHA", "alpha/**"),
					codeTaskCall("c2", "beta work", "TASK-BETA", "beta/**"),
					codeTaskCall("c3", "gamma work", "TASK-GAMMA", "gamma/**"),
				}},
				{Done: true},
			},
			{{TextDelta: "all done", Done: true}},
		},
		// Writes inside its lease and stops.
		"TASK-ALPHA": {
			{
				{ToolCalls: []provider.ToolCall{writeCall("a1", "alpha/a.txt", "alpha-after")}},
				{Done: true},
			},
			{{TextDelta: "alpha finished", Done: true}},
		},
		// Writes inside its lease, then reaches for a sibling's file and for a
		// mutating shell command. Both must be refused without stopping it.
		"TASK-BETA": {
			{
				{ToolCalls: []provider.ToolCall{
					writeCall("b1", "beta/b.txt", "beta-after"),
					writeCall("b2", "alpha/a.txt", "beta-was-here"),
					{ID: "b3", Name: tools.ShellToolName, Args: map[string]any{
						tools.ShellParamCommand: "rm -rf alpha",
					}},
				}},
				{Done: true},
			},
			{{TextDelta: "beta finished", Done: true}},
		},
		// Dies mid-turn. Its siblings' writes must survive.
		"TASK-GAMMA": {
			{{Error: errors.New("upstream exploded")}},
		},
	})

	parent := newPipelineRunner(t, root, gen, snapMgr)

	events := drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	// Alpha's write landed; beta's attempt on the same file was denied.
	assertFile(t, root, "alpha/a.txt", "alpha-after")
	assertFile(t, root, "beta/b.txt", "beta-after")

	if _, err := os.Stat(filepath.Join(root, "alpha")); err != nil {
		t.Fatalf("the shell command deleted a leased directory: %v", err)
	}

	// Gamma failed, but the batch still reported alpha and beta.
	results := toolResults(events, tools.CodeTaskToolName)
	if len(results) != 3 {
		t.Fatalf("want 3 code_task results, got %d: %+v", len(results), results)
	}
	var failed int
	for _, ev := range results {
		if ev.IsError {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("want exactly one failed subagent, got %d", failed)
	}
	// Beta reaching a second turn proves its two refusals came back as tool
	// results it could read, rather than killing it. An untouched file alone
	// would not distinguish a denial from a crash.
	if got := gen.turns("TASK-BETA"); got != 2 {
		t.Errorf("beta ran %d turns, want 2 (denials must not end the turn)", got)
	}
	if got := gen.turns("TASK-ALPHA"); got != 2 {
		t.Errorf("alpha ran %d turns, want 2", got)
	}

	// The parent is told which of its own reads went stale, and the two
	// successful children report the files they wrote.
	joined := joinEventText(results)
	for _, want := range []string{"alpha/a.txt", "beta/b.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("result cards do not name %q:\n%s", want, joined)
		}
	}

	// Parallel commits must leave one decodable JSONL record per write.
	records := readSnapshotIndex(t, root, sessionID)
	if len(records) != 2 {
		t.Fatalf("want 2 snapshot records, got %d", len(records))
	}

	restored, err := snapMgr.Undo(2)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("undo restored %d files, want 2: %v", len(restored), restored)
	}
	assertFile(t, root, "alpha/a.txt", "alpha-before")
	assertFile(t, root, "beta/b.txt", "beta-before")
}

// TestCodingSubagentOverlappingLeasesDeniedBeforeDispatch asserts the batch is
// refused before any child starts. Launching them and denying the second write
// later would leave the first child's work half-applied.
func TestCodingSubagentOverlappingLeasesDeniedBeforeDispatch(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()
	seedFile(t, root, "shared/x.txt", "original")

	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{
					codeTaskCall("c1", "first", "TASK-ONE", "shared/**"),
					codeTaskCall("c2", "second", "TASK-TWO", "shared/x.txt"),
				}},
				{Done: true},
			},
			{{TextDelta: "understood", Done: true}},
		},
		"TASK-ONE": {{{ToolCalls: []provider.ToolCall{writeCall("w", "shared/x.txt", "one")}}, {Done: true}}},
		"TASK-TWO": {{{ToolCalls: []provider.ToolCall{writeCall("w", "shared/x.txt", "two")}}, {Done: true}}},
	})

	snapMgr, err := snapshot.NewManager(root, "overlap", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	parent := newPipelineRunner(t, root, gen, snapMgr)

	events := drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	if gen.turns("TASK-ONE") != 0 || gen.turns("TASK-TWO") != 0 {
		t.Fatal("a child was launched despite overlapping leases")
	}
	assertFile(t, root, "shared/x.txt", "original")

	results := toolResults(events, tools.CodeTaskToolName)
	if len(results) != 2 {
		t.Fatalf("want 2 denials, got %d", len(results))
	}
	for _, ev := range results {
		if !ev.IsError || !strings.Contains(ev.Text, "overlapping write leases") {
			t.Errorf("denial does not explain the conflict: %+v", ev)
		}
	}
}

// TestCodingSubagentCannotNest keeps depth at 1. A tree of agents multiplies
// cost and makes a runaway loop hard to see from the parent transcript.
func TestCodingSubagentCannotNest(t *testing.T) {
	t.Setenv("SAGITTARIUS_HOME", t.TempDir())
	root := t.TempDir()

	gen := newRoutedGenerator(map[string][][]provider.StreamResponse{
		"PARENT": {
			{
				{ToolCalls: []provider.ToolCall{codeTaskCall("c1", "outer", "TASK-OUTER", "out/**")}},
				{Done: true},
			},
			{{TextDelta: "ok", Done: true}},
		},
		"TASK-OUTER": {
			{
				{ToolCalls: []provider.ToolCall{codeTaskCall("c2", "inner", "TASK-INNER", "out/deep/**")}},
				{Done: true},
			},
			{{TextDelta: "outer finished", Done: true}},
		},
	})

	snapMgr, err := snapshot.NewManager(root, "nest", snapshot.Options{})
	if err != nil {
		t.Fatalf("snapshot.NewManager: %v", err)
	}
	parent := newPipelineRunner(t, root, gen, snapMgr)
	drainToSlice(t, mustRunTurn(t, parent, "PARENT"))

	if gen.turns("TASK-INNER") != 0 {
		t.Fatal("a subagent launched a subagent")
	}
}

func newPipelineRunner(t *testing.T, root string, gen *routedGenerator, snapMgr *snapshot.Manager) *Runner {
	t.Helper()
	on := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Subagents: &config.SagittariusSubagents{
				Coding:   &config.SagittariusSubagentClass{Enabled: &on},
				Research: &config.SagittariusSubagentClass{Enabled: &on},
			},
		},
	}
	runner, err := NewRunner(RunnerConfig{
		Generator:    gen,
		Model:        "test-model",
		WorkDir:      root,
		ApprovalMode: ApprovalYolo,
		Interactive:  false,
		Settings:     settings,
		Snapshotter:  snapMgr,
		SubagentGenerator: func(context.Context, *config.Settings) (provider.ContentGenerator, error) {
			return gen, nil
		},
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if _, ok := runner.registry.Lookup(tools.CodeTaskToolName); !ok {
		t.Fatal("code_task was not registered")
	}
	return runner
}

func seedFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("seed %s: %v", rel, err)
	}
}

func assertFile(t *testing.T, root, rel, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", rel, got, want)
	}
}

func drainToSlice(t *testing.T, events <-chan ui.StreamEvent) []ui.StreamEvent {
	t.Helper()
	var out []ui.StreamEvent
	for ev := range events {
		out = append(out, ev)
	}
	return out
}

func toolResults(events []ui.StreamEvent, name string) []ui.StreamEvent {
	var out []ui.StreamEvent
	for _, ev := range events {
		if ev.Type == ui.StreamToolResult && ev.ToolName == name {
			out = append(out, ev)
		}
	}
	return out
}

func joinEventText(events []ui.StreamEvent) string {
	var b strings.Builder
	for _, ev := range events {
		fmt.Fprintln(&b, ev.Text)
	}
	return b.String()
}

// readSnapshotIndex decodes the session index, failing on any line that is not
// a complete JSON object — the exact damage two concurrent appends would do.
func readSnapshotIndex(t *testing.T, root, sessionID string) []map[string]any {
	t.Helper()
	tmp, err := storage.ProjectTmpDir(root)
	if err != nil {
		t.Fatalf("ProjectTmpDir: %v", err)
	}
	path := filepath.Join(tmp, "snapshots", sessionID+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer func() { _ = f.Close() }()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("corrupt snapshot index line %q: %v", line, err)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan index: %v", err)
	}
	return out
}
