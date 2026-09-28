package prompt

import (
	"strings"
	"testing"
)

// The charters are the only thing a child knows about its own situation, so a
// duplicated or missing rule is a behavior change, not a formatting nit. These
// anchor the rules that the scheduler actually enforces.
func TestResearchSubagentCharterRulesAppearOnce(t *testing.T) {
	got := ResearchSubagentCharter()
	for _, phrase := range []string{
		"You cannot modify anything.",
		"Your final message is the entire deliverable.",
		"Cite what you found.",
		"Report absence explicitly.",
		"Stop when the question is answered.",
	} {
		if n := strings.Count(got, phrase); n != 1 {
			t.Errorf("phrase %q appears %d times, want exactly 1", phrase, n)
		}
	}
	if strings.Contains(got, "write_paths") {
		t.Error("the research charter must not mention a write lease")
	}
}

func TestCodingSubagentCharterNamesTheLease(t *testing.T) {
	got := CodingSubagentCharter([]string{"internal/tools/**", "docs/subagents.md"}, "")
	for _, want := range []string{"internal/tools/**", "docs/subagents.md"} {
		if !strings.Contains(got, want) {
			t.Errorf("charter does not name leased pattern %q", want)
		}
	}
	for _, phrase := range []string{
		"Your write lease is exactly these paths:",
		"Siblings are editing other files right now.",
		"Read before you write.",
		"Your final message is the entire deliverable.",
		"Finish the change or say why you could not.",
	} {
		if n := strings.Count(got, phrase); n != 1 {
			t.Errorf("phrase %q appears %d times, want exactly 1", phrase, n)
		}
	}
	if strings.Contains(got, "Shared design contract") {
		t.Error("charter without a contract must not render the contract section")
	}
}

// An empty lease must read as a prohibition, not as an empty list the model can
// interpret as "unrestricted".
func TestCodingSubagentCharterEmptyLease(t *testing.T) {
	got := CodingSubagentCharter(nil, "")
	if !strings.Contains(got, "you may not write any file") {
		t.Errorf("empty lease must state the prohibition, got:\n%s", got)
	}
}

// The contract section is the only place a sibling learns the batch-wide
// decisions, so it must be present verbatim and framed as binding.
func TestCodingSubagentCharterRendersContract(t *testing.T) {
	contract := "Money is integer cents. The CLI layer rejects invalid input."
	got := CodingSubagentCharter([]string{"app/**"}, contract)
	if !strings.Contains(got, "## Shared design contract (binding)") {
		t.Error("charter with a contract must render the contract heading")
	}
	if !strings.Contains(got, contract) {
		t.Error("charter must carry the contract text verbatim")
	}
}

func TestReviewSubagentCharterRulesAppearOnce(t *testing.T) {
	got := ReviewSubagentCharter()
	for _, phrase := range []string{
		"You cannot modify anything.",
		"Close with a verdict line.",
		"VERDICT: PASS",
		"VERDICT: FAIL",
	} {
		if n := strings.Count(got, phrase); n != 1 {
			t.Errorf("phrase %q appears %d times, want exactly 1", phrase, n)
		}
	}
}
