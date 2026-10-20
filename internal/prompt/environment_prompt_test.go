package prompt

import (
	"regexp"
	"strings"
	"testing"
)

func TestRenderEnvironment(t *testing.T) {
	t.Parallel()
	repo := &Environment{
		Date: "2026-10-20", WorkDir: "/work/proj", Shell: "bash",
		Branch: "main", Status: " M a.go", RecentCommits: "abc123 first",
	}
	tests := []struct {
		name    string
		env     *Environment
		want    []string
		notWant []string
	}{
		{"nil omits section", nil, nil, []string{"## Environment"}},
		{"git repo", repo,
			[]string{"## Environment", "(date: 2026-10-20)", "run `git status`", "- Working directory: /work/proj",
				"- OS: Linux", "- Shell: bash", "- Git branch: main", " M a.go", "abc123 first"},
			nil},
		{"clean repo", &Environment{Date: "2026-10-20", Branch: "main"},
			[]string{"- Git status: clean"}, []string{"- Recent commits:"}},
		{"not a repo", &Environment{Date: "2026-10-20", WorkDir: "/tmp/x"},
			[]string{"- Working directory: /tmp/x"},
			[]string{"Git", "git status"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out := Build(Options{Personality: PersonalityProgrammer, OS: "linux", Environment: tt.env})
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q", w)
				}
			}
			section := environmentSection(out)
			for _, nw := range tt.notWant {
				if strings.Contains(section, nw) {
					t.Errorf("environment section must not contain %q:\n%s", nw, section)
				}
			}
			if regexp.MustCompile(`\b\d{1,2}:\d{2}\b`).MatchString(section) {
				t.Errorf("environment section must not carry a time of day:\n%s", section)
			}
		})
	}
}

func TestEnvironmentAppearsForEveryPersonality(t *testing.T) {
	t.Parallel()
	env := &Environment{Date: "2026-10-20", WorkDir: "/w"}
	for _, p := range []Personality{PersonalityProgrammer, PersonalitySysadmin, PersonalityPersonalAssistant, PersonalityCreativeAssistant} {
		for _, v := range []Variant{VariantFull, VariantLite} {
			out := Build(Options{Personality: p, Variant: v, Environment: env})
			if !strings.Contains(out, "## Environment") {
				t.Errorf("%s/%s: missing ## Environment", p, v)
			}
		}
	}
}

// environmentSection returns the ## Environment section of a built prompt.
func environmentSection(out string) string {
	_, after, ok := strings.Cut(out, "## Environment")
	if !ok {
		return ""
	}
	if i := strings.Index(after, "\n## "); i >= 0 {
		after = after[:i]
	}
	return after
}
