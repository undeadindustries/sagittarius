package skills

import (
	"strings"
	"testing"
)

// managerWith builds a manager holding defs without touching the filesystem
// discovery paths, so tests stay independent of the host's installed skills.
func managerWith(names ...string) *Manager {
	m := NewManager("/tmp", false)
	for _, n := range names {
		m.skills = append(m.skills, Definition{Name: n, Body: "body of " + n, Location: "/tmp/" + n + "/SKILL.md"})
	}
	return m
}

func TestContentDoesNotActivate(t *testing.T) {
	t.Parallel()
	m := managerWith("golang")

	out, err := m.Content("golang")
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if !strings.Contains(out, `<activated_skill name="golang">`) {
		t.Fatalf("missing wrapper: %q", out)
	}
	if !strings.Contains(out, "body of golang") {
		t.Fatalf("missing body: %q", out)
	}
	if m.IsActive("golang") {
		t.Fatal("Content must not mark the skill active")
	}
}

func TestActivateContentActivates(t *testing.T) {
	t.Parallel()
	m := managerWith("golang")

	if _, err := m.ActivateContent("golang"); err != nil {
		t.Fatalf("ActivateContent: %v", err)
	}
	if !m.IsActive("golang") {
		t.Fatal("ActivateContent must mark the skill active")
	}
}

func TestContentCaseInsensitive(t *testing.T) {
	t.Parallel()
	m := managerWith("Golang")

	if _, err := m.Content("golang"); err != nil {
		t.Fatalf("Content: %v", err)
	}
}

func TestContentUnknownSkillSuggests(t *testing.T) {
	t.Parallel()
	m := managerWith("golang", "google-apps-script", "postgres-engineering")

	_, err := m.Content("gol")
	if err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	msg := err.Error()
	if !strings.Contains(msg, `unknown skill "gol"`) {
		t.Fatalf("error should name the skill: %q", msg)
	}
	if !strings.Contains(msg, "golang") {
		t.Fatalf("error should suggest the prefix match: %q", msg)
	}
	if strings.Contains(msg, "postgres-engineering") {
		t.Fatalf("unrelated skills should not be suggested: %q", msg)
	}
}

func TestContentUnknownSkillCapsSuggestions(t *testing.T) {
	t.Parallel()
	m := managerWith("a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8")

	_, err := m.Content("a")
	if err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	// One entry plus maxSuggestions-1 separators: a hundred installed skills
	// must not produce a hundred-name error line.
	if got := strings.Count(err.Error(), ", "); got != maxSuggestions-1 {
		t.Fatalf("suggestion count = %d separators, want %d: %q", got, maxSuggestions-1, err.Error())
	}
}

func TestContentUnknownSkillNoMatches(t *testing.T) {
	t.Parallel()
	m := managerWith("golang")

	_, err := m.Content("zzz")
	if err == nil {
		t.Fatal("expected an error for an unknown skill")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Fatalf("no near match should omit the suggestion clause: %q", err.Error())
	}
}

func TestNearestNamesSkipsDisabled(t *testing.T) {
	t.Parallel()
	m := managerWith("golang")
	m.skills = append(m.skills, Definition{Name: "golang-disabled", Disabled: true})

	for _, n := range m.nearestNames("golang") {
		if n == "golang-disabled" {
			t.Fatal("disabled skills must not be suggested")
		}
	}
}
