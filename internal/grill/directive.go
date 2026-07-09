package grill

import (
	"fmt"
	"strings"
)

// DirectiveConfig tunes the interrogation directive from sagittarius.grill
// settings. The zero value (MaxQuestions 0, Recommend false) means "no soft
// cap" and "do not force a recommended default"; callers that want the
// documented default should set Recommend true explicitly.
type DirectiveConfig struct {
	// MaxQuestions is a soft cap on the number of questions. 0 means no cap.
	MaxQuestions int
	// Recommend requires each ask_user call to propose a recommended default
	// first (recommended_index). When false, the model presents neutral
	// options without steering toward one.
	Recommend bool
}

// Directive returns the system-prompt suffix that turns the agent into a
// skeptical senior-engineer interviewer for the given topic, tuned by cfg. It
// is appended after the mode suffix whenever a grill session is active.
func Directive(topic string, cfg DirectiveConfig) string {
	recommendBullet := "- Every question needs 2-4 concrete options, most-recommended first (set\n" +
		"  recommended_index). Recommend a real default — do not hedge with \"it\n" +
		"  depends\". Free-form answers are always still possible via the automatic\n" +
		"  \"Other\" option."
	if !cfg.Recommend {
		recommendBullet = "- Every question needs 2-4 concrete options. Present them neutrally without\n" +
			"  steering toward one (do not set recommended_index unless the user asks\n" +
			"  for your recommendation). Free-form answers are always still possible via\n" +
			"  the automatic \"Other\" option."
	}

	capBullet := ""
	if cfg.MaxQuestions > 0 {
		capBullet = fmt.Sprintf("\n- Aim to fully resolve this topic within about %d questions; as you approach\n"+
			"  that many, prioritize only the highest-impact remaining branches and wrap\n"+
			"  up rather than drilling into minor details.", cfg.MaxQuestions)
	}

	return fmt.Sprintf(`# Grill-me mode: interrogating "%s"

You are conducting a structured interview to fully specify this topic before
any code is written. Act as a skeptical senior engineer, not an eager intern:

- Ask exactly ONE question at a time using the ask_user tool. Never ask more
  than one question in a single turn, and never ask a question in plain text —
  always use ask_user so the user gets a structured picker.
- Pick the single highest-uncertainty open branch of the decision tree first:
  the question whose answer would most change the design if it went the other
  way.
- Before asking, explore the codebase (read_file, grep_search,
  list_directory) to answer anything you can verify yourself. Only ask the
  user about things genuinely unknowable from the code: product intent,
  priorities, and trade-offs.
%s
- Never re-ask a question whose answer is already decided or already evident
  from the code or the conversation so far.
- Push back once if the user's answer seems to conflict with an earlier
  decision or a real constraint you observed in the code; do not just agree
  to keep things moving.
- Do NOT write or edit any files and do NOT run mutating shell commands while
  interrogating. This is a read-only investigation and interview phase.
- Stop asking once every open branch is resolved, or once the user runs
  /grill done. Do not manufacture busywork questions to keep going.%s`, topic, recommendBullet, capBullet)
}

// SpecPrompt returns the final model-turn prompt that generates the spec
// markdown from the accumulated decisions once /grill done runs. specPath is
// the file the model should write.
func SpecPrompt(topic string, decisions []Decision, specPath string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The grill-me interrogation on %q is finished. Write a spec file at %s summarizing every resolved decision below as a clear, actionable specification (not a transcript). Use headings for major areas, note any open questions that were never resolved, and end with a short \"Next steps\" section. Use write_file to create it, then stop — do not implement the feature yet.\n\n", topic, specPath)
	if len(decisions) == 0 {
		b.WriteString("No decisions were recorded; write a spec noting that the interrogation ended before any questions were answered.\n")
		return b.String()
	}
	b.WriteString("Resolved decisions:\n\n")
	for i, d := range decisions {
		fmt.Fprintf(&b, "%d. Q: %s\n   A: %s\n", i+1, d.Question, d.Answer)
		if strings.TrimSpace(d.Rationale) != "" {
			fmt.Fprintf(&b, "   Rationale: %s\n", d.Rationale)
		}
	}
	return b.String()
}

// DefaultSpecDir is the directory the spec markdown is written into when
// sagittarius.grill.specDir is unset.
const DefaultSpecDir = "docs/specs"

// SlugTopic converts a free-form topic into a filesystem-safe slug for the
// spec filename (e.g. "Auth flow!" -> "auth-flow").
func SlugTopic(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	var b strings.Builder
	prevDash := false
	for _, r := range topic {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "topic"
	}
	return slug
}
