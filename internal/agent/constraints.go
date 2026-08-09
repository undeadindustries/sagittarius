package agent

import "strings"

// Constraints returns a copy of the active standing session constraints, or
// nil when none are set. The copy means callers never share the runner's live
// slice, so mutating the result cannot race a concurrent AddConstraint/
// ClearConstraints (which replace the slice under the write lock).
func (r *Runner) Constraints() []string {
	r.constraintsMu.RLock()
	defer r.constraintsMu.RUnlock()
	if len(r.constraints) == 0 {
		return nil
	}
	return append([]string(nil), r.constraints...)
}

// AddConstraint appends a user-stated scope limit (e.g. "do not touch
// AGENTS.md yet") to the standing list, persists it to the session recorder,
// and recomposes the system instruction so the constraint reaches the model on
// the very next turn. A duplicate (case-insensitive, trimmed) is a no-op: it
// still persists and recomposes are skipped so a repeated "/constraints <same
// text>" does not bloat the session file or the prompt.
func (r *Runner) AddConstraint(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	r.constraintsMu.Lock()
	for _, existing := range r.constraints {
		if strings.EqualFold(existing, text) {
			r.constraintsMu.Unlock()
			return nil
		}
	}
	r.constraints = append(r.constraints, text)
	snap := append([]string(nil), r.constraints...)
	r.constraintsMu.Unlock()

	if r.sessionRecorder != nil {
		if err := r.sessionRecorder.SetConstraints(snap); err != nil {
			return err
		}
	}
	r.applyModeSystemSuffix()
	return nil
}

// ClearConstraints removes every standing constraint, persists the cleared
// state, and recomposes the system instruction so the directive disappears
// from the very next request.
func (r *Runner) ClearConstraints() error {
	r.constraintsMu.Lock()
	if len(r.constraints) == 0 {
		r.constraintsMu.Unlock()
		return nil
	}
	r.constraints = nil
	r.constraintsMu.Unlock()

	if r.sessionRecorder != nil {
		if err := r.sessionRecorder.SetConstraints([]string{}); err != nil {
			return err
		}
	}
	r.applyModeSystemSuffix()
	return nil
}

// renderConstraintsDirective builds the system-prompt suffix for standing
// constraints. It is deliberately blunt about precedence: constraints exist
// specifically to survive a request that would otherwise trigger the
// tool-invocation mandate's "act now" bias.
func renderConstraintsDirective(constraints []string) string {
	var b strings.Builder
	b.WriteString("**Active session constraints.** These override every other instruction in this ")
	b.WriteString("prompt, including the tool-invocation mandate above. If a request appears to require ")
	b.WriteString("violating one, stop and say so instead of proceeding:\n")
	for _, c := range constraints {
		b.WriteString("- " + c + "\n")
	}
	b.WriteString("These stay in force until the user lifts them (`/constraints clear`).")
	return b.String()
}
