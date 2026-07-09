// Package grill implements "grill-me" mode: a Socratic-interrogation session
// where the agent interviews the user about a topic one structured question at
// a time instead of running autonomously (contrast internal/goal). It mirrors
// the shape of internal/goal so the two features share the same runner/slash/
// session-persistence seams.
package grill

import "time"

// Status is the lifecycle state of a grill session.
type Status string

const (
	// StatusActive is interrogating: the agent should be asking questions.
	StatusActive Status = "active"
	// StatusPaused suspends interrogation; tools stay read-only gated.
	StatusPaused Status = "paused"
	// StatusSummarizing lifts the read-only gate so the final spec write can
	// succeed, while the session is still considered open.
	StatusSummarizing Status = "summarizing"
	// StatusComplete means the spec was generated and the session is done.
	StatusComplete Status = "complete"
)

// Decision records one resolved question/answer pair from the interrogation.
type Decision struct {
	Question  string
	Answer    string
	Rationale string
}

// Session is the runtime state of one grill-me interrogation.
type Session struct {
	Topic         string
	Status        Status
	QuestionCount int
	StartedAt     time.Time
	Decisions     []Decision
	SpecPath      string
	Note          string
}

// DecisionSnapshot is the JSON-serializable form of a Decision.
type DecisionSnapshot struct {
	Question  string `json:"question"`
	Answer    string `json:"answer"`
	Rationale string `json:"rationale,omitempty"`
}

// Snapshot is the JSON-serializable subset of Session for session persistence.
type Snapshot struct {
	Topic         string             `json:"topic"`
	Status        string             `json:"status"`
	QuestionCount int                `json:"questionCount"`
	StartedAt     string             `json:"startedAt"`
	Decisions     []DecisionSnapshot `json:"decisions,omitempty"`
	SpecPath      string             `json:"specPath,omitempty"`
	Note          string             `json:"note,omitempty"`
}

// Clone returns a deep copy of the session (including its Decisions slice) so
// callers can read or mutate it without racing the runner's shared instance.
// Returns nil for a nil Session.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	c := *s
	if s.Decisions != nil {
		c.Decisions = make([]Decision, len(s.Decisions))
		copy(c.Decisions, s.Decisions)
	}
	return &c
}

// ToSnapshot converts a Session to a Snapshot. Returns nil for a nil Session.
func (s *Session) ToSnapshot() *Snapshot {
	if s == nil {
		return nil
	}
	decisions := make([]DecisionSnapshot, 0, len(s.Decisions))
	for _, d := range s.Decisions {
		decisions = append(decisions, DecisionSnapshot{
			Question:  d.Question,
			Answer:    d.Answer,
			Rationale: d.Rationale,
		})
	}
	return &Snapshot{
		Topic:         s.Topic,
		Status:        string(s.Status),
		QuestionCount: s.QuestionCount,
		StartedAt:     s.StartedAt.UTC().Format(time.RFC3339Nano),
		Decisions:     decisions,
		SpecPath:      s.SpecPath,
		Note:          s.Note,
	}
}

// FromSnapshot creates a Session from a Snapshot. Returns nil for a nil Snapshot.
func FromSnapshot(s *Snapshot) *Session {
	if s == nil {
		return nil
	}
	startedAt, err := time.Parse(time.RFC3339Nano, s.StartedAt)
	if err != nil {
		startedAt = time.Now()
	}
	decisions := make([]Decision, 0, len(s.Decisions))
	for _, d := range s.Decisions {
		decisions = append(decisions, Decision{
			Question:  d.Question,
			Answer:    d.Answer,
			Rationale: d.Rationale,
		})
	}
	return &Session{
		Topic:         s.Topic,
		Status:        Status(s.Status),
		QuestionCount: s.QuestionCount,
		StartedAt:     startedAt,
		Decisions:     decisions,
		SpecPath:      s.SpecPath,
		Note:          s.Note,
	}
}
