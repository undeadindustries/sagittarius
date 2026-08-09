package atmention

import (
	"fmt"
	"strings"
	"testing"
)

// fakeResolver serves canned skill bodies and records lookups.
type fakeResolver struct {
	bodies map[string]string
	calls  []string
}

func (f *fakeResolver) Content(name string) (string, error) {
	f.calls = append(f.calls, name)
	body, ok := f.bodies[strings.ToLower(name)]
	if !ok {
		return "", fmt.Errorf("unknown skill %q", name)
	}
	return fmt.Sprintf("<activated_skill name=%q>\n%s\n</activated_skill>", name, body), nil
}

func newResolver(bodies map[string]string) *fakeResolver {
	lowered := make(map[string]string, len(bodies))
	for k, v := range bodies {
		lowered[strings.ToLower(k)] = v
	}
	return &fakeResolver{bodies: lowered}
}

func TestExpandInjectsSkill(t *testing.T) {
	ws := newWorkspace(t, nil)
	res := newResolver(map[string]string{"golang": "prefer errors.Is"})

	parts, err := Expand(ws, "use @skill:golang here", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("got %d parts, want 2", len(parts))
	}
	if parts[0].Text != "use @skill:golang here" {
		t.Fatalf("first part = %q, want the original query", parts[0].Text)
	}
	body := parts[1].Text
	for _, want := range []string{skillHeader, skillFooter, "prefer errors.Is", `<activated_skill name="golang">`} {
		if !strings.Contains(body, want) {
			t.Fatalf("skill block missing %q: %q", want, body)
		}
	}
	if !strings.Contains(body, `Do not call activate_skill for "golang".`) {
		t.Fatalf("skill block missing the redundant-call guard: %q", body)
	}
}

func TestExpandSkillComesAfterFiles(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"a.txt": "FILE BODY"})
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	parts, err := Expand(ws, "@a.txt @skill:golang", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3 (query, files, skills)", len(parts))
	}
	if !strings.Contains(parts[1].Text, "FILE BODY") {
		t.Fatalf("second part should be the file block: %q", parts[1].Text)
	}
	// Instructions must sit closest to the generation point so a large file
	// reference cannot bury them.
	if !strings.Contains(parts[2].Text, "SKILL BODY") {
		t.Fatalf("third part should be the skill block: %q", parts[2].Text)
	}
}

func TestExpandSkillOrderIndependentOfMentionOrder(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"a.txt": "FILE BODY"})
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	parts, err := Expand(ws, "@skill:golang @a.txt", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3", len(parts))
	}
	if !strings.Contains(parts[1].Text, "FILE BODY") || !strings.Contains(parts[2].Text, "SKILL BODY") {
		t.Fatalf("skills must stay last regardless of mention order: %+v", parts)
	}
}

func TestExpandSkillDeduplicates(t *testing.T) {
	ws := newWorkspace(t, nil)
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	parts, err := Expand(ws, "@skill:golang and @skill:GoLang", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if got := strings.Count(parts[1].Text, "SKILL BODY"); got != 1 {
		t.Fatalf("skill body appears %d times, want 1: %q", got, parts[1].Text)
	}
	if len(res.calls) != 1 {
		t.Fatalf("resolver called %d times, want 1: %v", len(res.calls), res.calls)
	}
}

func TestExpandUnknownSkillErrors(t *testing.T) {
	ws := newWorkspace(t, nil)
	res := newResolver(nil)

	_, err := Expand(ws, "@skill:nope", res)
	if err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	if !strings.Contains(err.Error(), "@skill:nope") {
		t.Fatalf("error should quote the mention: %v", err)
	}
}

func TestExpandSkillWithoutResolverErrors(t *testing.T) {
	ws := newWorkspace(t, nil)

	_, err := Expand(ws, "@skill:golang", nil)
	if err == nil {
		t.Fatal("expected an error when no skill manager is attached")
	}
	if !strings.Contains(err.Error(), "skills are not available") {
		t.Fatalf("error should explain the cause: %v", err)
	}
}

func TestExpandSkillWithoutWorkspaceStillWorks(t *testing.T) {
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	parts, err := Expand(nil, "@skill:golang", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 2 || !strings.Contains(parts[1].Text, "SKILL BODY") {
		t.Fatalf("skill mentions should not need a workspace: %+v", parts)
	}
}

func TestExpandBareSkillPrefixIsAPath(t *testing.T) {
	ws := newWorkspace(t, nil)
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	// "@skill:" with no name falls through to path resolution, which fails
	// loudly rather than silently dropping the token.
	if _, err := Expand(ws, "@skill:", res); err == nil {
		t.Fatal("expected a path error for a bare @skill: token")
	}
	if len(res.calls) != 0 {
		t.Fatalf("resolver should not be consulted: %v", res.calls)
	}
}

func TestExpandSkillTruncatesOversizedBody(t *testing.T) {
	ws := newWorkspace(t, nil)
	res := newResolver(map[string]string{"huge": strings.Repeat("x", perFileCap+1000)})

	parts, err := Expand(ws, "@skill:huge", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	body := parts[1].Text
	if len(body) > perFileCap+2048 {
		t.Fatalf("body not capped: %d bytes", len(body))
	}
	if !strings.Contains(body, "... (truncated)") {
		t.Fatal("truncated body should be marked")
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.Split(body, "The user explicitly")[0]), "</activated_skill>") {
		t.Fatalf("truncation must re-close the tag: %q", body[len(body)-200:])
	}
}

func TestExpandSkillSharesBudgetWithFiles(t *testing.T) {
	// A skill and a file both draw from combinedCap; the skill is resolved
	// first so an explicit request is never starved by a large file.
	big := strings.Repeat("y", combinedCap)
	ws := newWorkspace(t, map[string]string{"big.txt": big})
	res := newResolver(map[string]string{"golang": "SKILL BODY"})

	parts, err := Expand(ws, "@big.txt @skill:golang", res)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("got %d parts, want 3", len(parts))
	}
	if !strings.Contains(parts[2].Text, "SKILL BODY") {
		t.Fatal("skill body should survive a budget-consuming file")
	}
	total := len(parts[1].Text) + len(parts[2].Text)
	if total > combinedCap+4096 {
		t.Fatalf("combined injection = %d bytes, want roughly <= %d", total, combinedCap)
	}
}

func TestCapStringRuneBoundary(t *testing.T) {
	t.Parallel()
	// A multi-byte rune straddling the limit must not be split.
	s := strings.Repeat("é", 10)
	got, truncated := capString(s, 5)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if len(got) != 4 {
		t.Fatalf("cut = %d bytes, want 4 (back off to the rune boundary)", len(got))
	}
}

func TestCapStringExhaustedBudget(t *testing.T) {
	t.Parallel()
	got, truncated := capString("anything", 0)
	if got != "" || !truncated {
		t.Fatalf("capString with no budget = (%q, %v), want (\"\", true)", got, truncated)
	}
}

func TestCompleteSkillNames(t *testing.T) {
	names := func() []string { return []string{"golang", "google-apps-script", "postgres-engineering"} }
	idx := NewIndex(nil, names)

	input := "use @skill:go"
	comp := idx.Complete(input, len(input))
	if len(comp.Items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(comp.Items), comp.Items)
	}
	wantFrom := strings.Index(input, "@") + 1 + len(skillPrefix)
	if comp.ReplaceFrom != wantFrom {
		t.Fatalf("ReplaceFrom = %d, want %d (just past the prefix)", comp.ReplaceFrom, wantFrom)
	}
	if comp.Items[0].Insert != "golang" {
		t.Fatalf("first suggestion = %q, want golang", comp.Items[0].Insert)
	}
}

func TestCompleteOffersSkillPrefix(t *testing.T) {
	names := func() []string { return []string{"golang"} }
	idx := NewIndex(nil, names)

	for _, input := range []string{"@", "@sk", "@skil"} {
		comp := idx.Complete(input, len(input))
		if len(comp.Items) == 0 || comp.Items[0].Insert != skillPrefix {
			t.Fatalf("input %q should offer %q first, got %+v", input, skillPrefix, comp.Items)
		}
		if comp.Items[0].AppendSpace {
			t.Fatalf("input %q: the prefix must not append a space", input)
		}
		if comp.ReplaceFrom != 1 {
			t.Fatalf("input %q: ReplaceFrom = %d, want 1", input, comp.ReplaceFrom)
		}
	}
}

func TestCompleteSkillCaseInsensitivePrefix(t *testing.T) {
	names := func() []string { return []string{"golang"} }
	idx := NewIndex(nil, names)

	input := "@SKILL:gol"
	comp := idx.Complete(input, len(input))
	if len(comp.Items) != 1 || comp.Items[0].Insert != "golang" {
		t.Fatalf("got %+v, want golang", comp.Items)
	}
}

func TestCompleteSkillNoSourceIsEmpty(t *testing.T) {
	ws := newWorkspace(t, map[string]string{"a.go": "x"})
	idx := NewIndex(ws, nil)

	input := "@skill:go"
	if comp := idx.Complete(input, len(input)); len(comp.Items) != 0 {
		t.Fatalf("expected no items without a skill source, got %+v", comp.Items)
	}
}

// TestCompleteSkillNamesCaseInsensitiveOrder asserts the completion list is
// alphabetized case-insensitively, so a user who types lowercase sees lowercase
// results and capitalized names are not stranded at the top by byte-order sort.
func TestCompleteSkillNamesCaseInsensitiveOrder(t *testing.T) {
	names := func() []string {
		return []string{"6502-assembly", "Bash", "golang", "CSS-engineering", "bubbletea-engineering"}
	}
	idx := NewIndex(nil, names)

	input := "@skill:"
	comp := idx.Complete(input, len(input))
	want := []string{"6502-assembly", "Bash", "bubbletea-engineering", "CSS-engineering", "golang"}
	if len(comp.Items) != len(want) {
		t.Fatalf("got %d items, want %d: %+v", len(comp.Items), len(want), comp.Items)
	}
	for i, w := range want {
		if comp.Items[i].Insert != w {
			t.Fatalf("item %d = %q, want %q (full list: %+v)", i, comp.Items[i].Insert, w, comp.Items)
		}
	}
}
