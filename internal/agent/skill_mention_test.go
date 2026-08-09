package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

const skillMentionBody = "Always run gofmt before declaring the task done."

// runtimeWithSkill builds a Runtime whose catalog carries a single discovered
// skill in workDir. HOME is redirected so discovery cannot pick up the host
// user's installed skills.
func runtimeWithSkill(t *testing.T, workDir, name string) *Runtime {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	dir := filepath.Join(workDir, ".agents", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	doc := "---\nname: " + name + "\ndescription: a test skill\n---\n" + skillMentionBody + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(doc), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	mgr := skills.NewManager(workDir, true)
	if err := mgr.Discover(context.Background(), nil); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if mgr.Get(name) == nil {
		t.Fatalf("skill %q was not discovered", name)
	}

	ws, err := tools.NewWorkspace(workDir)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	cat, err := NewCatalog(CatalogConfig{Workspace: ws, Skills: mgr})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return &Runtime{Catalog: cat}
}

// TestRunTurnInjectsSkillMention asserts an "@skill:name" mention reaches the
// model as an extra message part while the session transcript keeps only the
// raw text the user typed.
func TestRunTurnInjectsSkillMention(t *testing.T) {
	workDir := t.TempDir()
	rt := runtimeWithSkill(t, workDir, "test-skill")

	chatsDir, err := session.ChatsDir(workDir)
	if err != nil {
		t.Fatalf("session.ChatsDir: %v", err)
	}
	rec := session.NewRecorder(chatsDir, "skill-mention-session", "hash", "main")

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{TextDelta: "ok"}, {Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Runtime:         rt,
		Generator:       gen,
		Model:           "test-model",
		WorkDir:         workDir,
		Interactive:     false,
		SessionRecorder: rec,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	const input = "@skill:test-skill tidy this up"
	drainEvents(t, mustRunTurn(t, runner, input))

	history := runner.History()
	if len(history) == 0 {
		t.Fatal("no history recorded")
	}
	user := history[0]
	if user.Role != provider.RoleUser {
		t.Fatalf("first message role = %v, want user", user.Role)
	}
	if len(user.Parts) != 2 {
		t.Fatalf("user message has %d parts, want 2 (query + skill block)", len(user.Parts))
	}
	if user.Parts[0].Text != input {
		t.Fatalf("first part = %q, want the raw input", user.Parts[0].Text)
	}
	if !strings.Contains(user.Parts[1].Text, skillMentionBody) {
		t.Fatalf("skill body missing from the model-bound parts: %q", user.Parts[1].Text)
	}

	// The skill is conversation content for this turn only; it must never land
	// in the system instruction, which is re-sent on every subsequent turn.
	req := gen.lastRequest()
	if req == nil {
		t.Fatal("no provider request captured")
	}
	if strings.Contains(req.SystemInstruction, skillMentionBody) {
		t.Fatal("skill body leaked into the system instruction")
	}

	// The transcript keeps what the user typed, not the expansion.
	transcript := readOnlySessionFile(t, chatsDir)
	if !strings.Contains(transcript, "@skill:test-skill tidy this up") {
		t.Fatalf("session transcript missing the raw user text: %s", transcript)
	}
	if strings.Contains(transcript, skillMentionBody) {
		t.Fatalf("session transcript should not carry the expanded skill body: %s", transcript)
	}
}

// readOnlySessionFile returns the contents of the single JSONL transcript in
// chatsDir. The recorder derives its own timestamped filename, so the test
// locates the file rather than reconstructing the name.
func readOnlySessionFile(t *testing.T, chatsDir string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(chatsDir, "*.jsonl"))
	if err != nil {
		t.Fatalf("glob session files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("found %d session files in %s, want 1", len(matches), chatsDir)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	return string(data)
}

// TestRunTurnUnknownSkillAbortsTurn asserts an unresolvable skill surfaces an
// error instead of silently dropping the requested context.
func TestRunTurnUnknownSkillAbortsTurn(t *testing.T) {
	workDir := t.TempDir()
	rt := runtimeWithSkill(t, workDir, "test-skill")

	gen := &fakeGenerator{batches: [][]provider.StreamResponse{{{Done: true}}}}
	runner, err := NewRunner(RunnerConfig{
		Runtime:     rt,
		Generator:   gen,
		Model:       "test-model",
		WorkDir:     workDir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events := collectEvents(t, mustRunTurn(t, runner, "@skill:nonexistent go"))
	var sawError bool
	for _, ev := range events {
		if ev.Err != nil && strings.Contains(ev.Err.Error(), "@skill:nonexistent") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatalf("expected a surfaced error for the unknown skill, got %+v", events)
	}
	if len(runner.History()) != 0 {
		t.Fatal("a failed expansion must not append the turn to history")
	}
}

// TestSkillNamesWithoutCatalog guards the typed-nil interface trap: a runner
// with no runtime must report no resolver and no names rather than panicking.
func TestSkillNamesWithoutCatalog(t *testing.T) {
	t.Parallel()

	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test-model",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if got := runner.skillResolver(); got != nil {
		t.Fatalf("skillResolver = %v, want nil", got)
	}
	if got := runner.SkillNames(); got != nil {
		t.Fatalf("SkillNames = %v, want nil", got)
	}
}
