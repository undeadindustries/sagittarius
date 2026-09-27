package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// claimSubagentAttempt records one delegation of a task and reports which
// attempt this is. The task identity is class + normalized description + sorted
// lease patterns, so re-delegating the same work after a failure counts up
// while genuinely new work starts fresh. When the count passes the configured
// cap the launch is denied before any child starts, telling the parent to
// finish the task itself instead of delegating a third time.
//
// The map is session state: /clear and session rotation wipe it (see
// ClearHistory and RotateSession). A denial does not increment the count.
func (r *Runner) claimSubagentAttempt(class config.SubagentClass, desc string, lease []string) (attempt, maxAttempts int, err error) {
	maxAttempts = config.ResolveSubagentMaxAttempts(r.sagittariusSettings(), config.DefaultSubagentMaxAttempts)
	if maxAttempts == 0 {
		return 1, 0, nil
	}
	key := subagentAttemptKey(class, desc, lease)
	r.subagentAttemptsMu.Lock()
	defer r.subagentAttemptsMu.Unlock()
	if r.subagentAttempts == nil {
		r.subagentAttempts = make(map[string]int)
	}
	n := r.subagentAttempts[key] + 1
	if n > maxAttempts {
		return n - 1, maxAttempts, fmt.Errorf(
			"subagent %s %q reached max attempts (%d): finish it yourself instead of delegating again",
			class, shortenAttemptDesc(desc), maxAttempts)
	}
	r.subagentAttempts[key] = n
	return n, maxAttempts, nil
}

// releaseSubagentAttempt gives back an attempt claimed for a child that never
// started (routing pin, credential, or runner construction failed). The
// budget exists to stop a model re-delegating work a child failed at; a
// configuration failure is not such an attempt and must not consume one.
// A no-op when the cap is disabled or the key holds no claims.
func (r *Runner) releaseSubagentAttempt(class config.SubagentClass, desc string, lease []string) {
	if config.ResolveSubagentMaxAttempts(r.sagittariusSettings(), config.DefaultSubagentMaxAttempts) == 0 {
		return
	}
	key := subagentAttemptKey(class, desc, lease)
	r.subagentAttemptsMu.Lock()
	defer r.subagentAttemptsMu.Unlock()
	switch n := r.subagentAttempts[key]; {
	case n > 1:
		r.subagentAttempts[key] = n - 1
	case n == 1:
		delete(r.subagentAttempts, key)
	}
}

// resetSubagentAttempts wipes the delegation counts. Called on /clear and
// session rotation so a new session starts every task fresh.
func (r *Runner) resetSubagentAttempts() {
	r.subagentAttemptsMu.Lock()
	defer r.subagentAttemptsMu.Unlock()
	r.subagentAttempts = make(map[string]int)
}

// subagentAttemptKey hashes the delegation identity. Descriptions are
// lowercased and collapsed to one line so trivial rewording still counts;
// coding tasks additionally key on the sorted lease, which is stable across
// retries of the same change.
func subagentAttemptKey(class config.SubagentClass, desc string, lease []string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(desc), " "))
	patterns := append([]string(nil), lease...)
	sort.Strings(patterns)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s", class, norm, strings.Join(patterns, "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}

func shortenAttemptDesc(desc string) string {
	const maxAttemptDescRunes = 80
	d := strings.Join(strings.Fields(desc), " ")
	if len([]rune(d)) <= maxAttemptDescRunes {
		return d
	}
	runes := []rune(d)
	return string(runes[:maxAttemptDescRunes-1]) + "…"
}
