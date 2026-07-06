package goal

import "time"

// Status represents the lifecycle state of a goal.
type Status string

const (
	StatusActive        Status = "active"
	StatusPaused        Status = "paused"
	StatusBlocked       Status = "blocked"
	StatusBudgetLimited Status = "budget_limited"
	StatusComplete      Status = "complete"
)

// Goal represents the runtime state of a session objective.
type Goal struct {
	Objective      string
	Status         Status
	StartedAt      time.Time
	TurnCount      int
	MaxTurns       int
	TokenBudget    *int
	TokensBaseline int
	LastReason     string
	Note           string
}

// Snapshot is the JSON-serializable subset of Goal for session persistence.
type Snapshot struct {
	Objective      string `json:"objective"`
	Status         string `json:"status"`
	StartedAt      string `json:"startedAt"`
	TurnCount      int    `json:"turnCount"`
	MaxTurns       int    `json:"maxTurns"`
	TokenBudget    *int   `json:"tokenBudget,omitempty"`
	TokensBaseline int    `json:"tokensBaseline"`
	LastReason     string `json:"lastReason,omitempty"`
	Note           string `json:"note,omitempty"`
}

// ToSnapshot converts a Goal to a Snapshot.
func (g *Goal) ToSnapshot() *Snapshot {
	if g == nil {
		return nil
	}
	return &Snapshot{
		Objective:      g.Objective,
		Status:         string(g.Status),
		StartedAt:      g.StartedAt.UTC().Format(time.RFC3339Nano),
		TurnCount:      g.TurnCount,
		MaxTurns:       g.MaxTurns,
		TokenBudget:    g.TokenBudget,
		TokensBaseline: g.TokensBaseline,
		LastReason:     g.LastReason,
		Note:           g.Note,
	}
}

// FromSnapshot creates a Goal from a Snapshot.
func FromSnapshot(s *Snapshot) *Goal {
	if s == nil {
		return nil
	}
	startedAt, err := time.Parse(time.RFC3339Nano, s.StartedAt)
	if err != nil {
		startedAt = time.Now()
	}
	return &Goal{
		Objective:      s.Objective,
		Status:         Status(s.Status),
		StartedAt:      startedAt,
		TurnCount:      s.TurnCount,
		MaxTurns:       s.MaxTurns,
		TokenBudget:    s.TokenBudget,
		TokensBaseline: s.TokensBaseline,
		LastReason:     s.LastReason,
		Note:           s.Note,
	}
}
