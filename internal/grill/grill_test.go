package grill

import (
	"strings"
	"testing"
	"time"
)

func TestDirectiveNonEmptyAndMentionsTopic(t *testing.T) {
	d := Directive("auth flow", DirectiveConfig{Recommend: true})
	if strings.TrimSpace(d) == "" {
		t.Fatal("Directive() returned empty string")
	}
	if !strings.Contains(d, "auth flow") {
		t.Errorf("Directive() = %q, want it to mention the topic", d)
	}
	if !strings.Contains(strings.ToLower(d), "ask_user") {
		t.Error("Directive() should instruct the model to use ask_user")
	}
	if !strings.Contains(strings.ToLower(d), "one question") {
		t.Error("Directive() should instruct one question at a time")
	}
}

func TestDirectiveRecommendToggle(t *testing.T) {
	on := Directive("auth flow", DirectiveConfig{Recommend: true})
	if !strings.Contains(on, "recommended_index") || !strings.Contains(on, "Recommend a real default") {
		t.Errorf("Recommend=true directive should require a recommended default, got %q", on)
	}

	off := Directive("auth flow", DirectiveConfig{Recommend: false})
	if strings.Contains(off, "Recommend a real default") {
		t.Errorf("Recommend=false directive should not require a recommended default, got %q", off)
	}
	if !strings.Contains(off, "neutrally") {
		t.Errorf("Recommend=false directive should present options neutrally, got %q", off)
	}
}

func TestDirectiveMaxQuestions(t *testing.T) {
	none := Directive("auth flow", DirectiveConfig{Recommend: true})
	if strings.Contains(none, "within about") {
		t.Errorf("MaxQuestions=0 should not add a soft-cap bullet, got %q", none)
	}

	capped := Directive("auth flow", DirectiveConfig{Recommend: true, MaxQuestions: 7})
	if !strings.Contains(capped, "within about 7 questions") {
		t.Errorf("MaxQuestions=7 should mention the soft cap, got %q", capped)
	}
}

func TestSpecPromptFormatting(t *testing.T) {
	decisions := []Decision{
		{Question: "Storage?", Answer: "Postgres", Rationale: "already used elsewhere"},
		{Question: "Auth?", Answer: "OAuth"},
	}
	prompt := SpecPrompt("auth flow", decisions, "docs/specs/auth-flow.md")
	if !strings.Contains(prompt, "docs/specs/auth-flow.md") {
		t.Error("SpecPrompt should mention the target file path")
	}
	if !strings.Contains(prompt, "Storage?") || !strings.Contains(prompt, "Postgres") {
		t.Error("SpecPrompt should include the recorded decisions")
	}
	if !strings.Contains(prompt, "already used elsewhere") {
		t.Error("SpecPrompt should include rationale when present")
	}
}

func TestSpecPromptNoDecisions(t *testing.T) {
	prompt := SpecPrompt("empty topic", nil, "docs/specs/empty-topic.md")
	if !strings.Contains(strings.ToLower(prompt), "no decisions") {
		t.Errorf("SpecPrompt with no decisions should say so, got %q", prompt)
	}
}

func TestSlugTopic(t *testing.T) {
	cases := map[string]string{
		"Auth flow!":        "auth-flow",
		"  spaced out  ":    "spaced-out",
		"already-slugged":   "already-slugged",
		"":                  "topic",
		"???":               "topic",
		"Mixed_Case 123 Go": "mixed-case-123-go",
	}
	for in, want := range cases {
		if got := SlugTopic(in); got != want {
			t.Errorf("SlugTopic(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	started := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	s := &Session{
		Topic:         "auth flow",
		Status:        StatusActive,
		QuestionCount: 2,
		StartedAt:     started,
		Decisions: []Decision{
			{Question: "Storage?", Answer: "Postgres", Rationale: "existing infra"},
		},
		SpecPath: "docs/specs/auth-flow.md",
		Note:     "paused for lunch",
	}

	snap := s.ToSnapshot()
	if snap == nil {
		t.Fatal("ToSnapshot() returned nil")
	}
	restored := FromSnapshot(snap)
	if restored == nil {
		t.Fatal("FromSnapshot() returned nil")
	}
	if restored.Topic != s.Topic || restored.Status != s.Status ||
		restored.QuestionCount != s.QuestionCount || restored.SpecPath != s.SpecPath ||
		restored.Note != s.Note {
		t.Errorf("round trip mismatch: got %+v, want %+v", restored, s)
	}
	if !restored.StartedAt.Equal(started) {
		t.Errorf("StartedAt round trip: got %v, want %v", restored.StartedAt, started)
	}
	if len(restored.Decisions) != 1 || restored.Decisions[0].Answer != "Postgres" {
		t.Errorf("Decisions round trip mismatch: got %+v", restored.Decisions)
	}
}

func TestSnapshotNil(t *testing.T) {
	var s *Session
	if s.ToSnapshot() != nil {
		t.Error("nil Session.ToSnapshot() should return nil")
	}
	if FromSnapshot(nil) != nil {
		t.Error("FromSnapshot(nil) should return nil")
	}
}

func TestCloneIsDeepCopy(t *testing.T) {
	var nilSession *Session
	if nilSession.Clone() != nil {
		t.Error("nil Session.Clone() should return nil")
	}

	orig := &Session{
		Topic:         "auth flow",
		Status:        StatusActive,
		QuestionCount: 1,
		Decisions:     []Decision{{Question: "Storage?", Answer: "Postgres"}},
	}
	clone := orig.Clone()
	if clone == orig {
		t.Fatal("Clone() must return a distinct pointer")
	}

	// Mutating the clone must not touch the original's backing array.
	clone.Status = StatusPaused
	clone.Decisions[0].Answer = "MySQL"
	clone.Decisions = append(clone.Decisions, Decision{Question: "Auth?", Answer: "OAuth"})

	if orig.Status != StatusActive {
		t.Errorf("original Status mutated to %q", orig.Status)
	}
	if orig.Decisions[0].Answer != "Postgres" {
		t.Errorf("original Decision mutated to %q", orig.Decisions[0].Answer)
	}
	if len(orig.Decisions) != 1 {
		t.Errorf("original Decisions grew to %d entries", len(orig.Decisions))
	}
}
