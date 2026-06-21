package agents

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// ReloadSummary reports agent discovery results from /agents reload.
type ReloadSummary struct {
	TotalLoaded int
	LocalCount  int
	RemoteCount int
	NewAgents   []string
	Errors      []string
}

// Registry discovers local agent definitions (execution deferred to Phase 13+).
type Registry struct {
	mu       sync.RWMutex
	workDir  string
	trusted  bool
	agents   []Definition
	previous map[string]struct{}
}

// NewRegistry constructs an agent registry for workDir.
func NewRegistry(workDir string, trusted bool) *Registry {
	return &Registry{workDir: workDir, trusted: trusted, previous: make(map[string]struct{})}
}

// Reload rescans user and project agent directories plus extension agents.
func (r *Registry) Reload(ctx context.Context, extensionAgents []Definition) (ReloadSummary, error) {
	_ = ctx
	r.mu.Lock()
	defer r.mu.Unlock()

	before := make(map[string]struct{}, len(r.agents))
	for _, a := range r.agents {
		before[a.Name] = struct{}{}
	}

	var collected []Definition
	var errors []string

	if homeAgents, err := userAgentsDir(); err == nil {
		agents, loadErrs := LoadFromDirectory(homeAgents)
		collected = append(collected, agents...)
		for _, e := range loadErrs {
			errors = append(errors, e.Error())
		}
	}
	if r.trusted {
		projectAgents := filepath.Join(r.workDir, config.SagittariusDir, "agents")
		agents, loadErrs := LoadFromDirectory(projectAgents)
		collected = append(collected, agents...)
		for _, e := range loadErrs {
			errors = append(errors, e.Error())
		}
	}
	collected = append(collected, extensionAgents...)

	byName := make(map[string]Definition)
	for _, agent := range collected {
		byName[agent.Name] = agent
	}
	r.agents = make([]Definition, 0, len(byName))
	for _, agent := range byName {
		r.agents = append(r.agents, agent)
	}

	summary := ReloadSummary{
		TotalLoaded: len(r.agents),
		LocalCount:  len(r.agents),
		Errors:      errors,
	}
	for _, agent := range r.agents {
		if _, ok := before[agent.Name]; !ok {
			summary.NewAgents = append(summary.NewAgents, agent.Name)
		}
	}
	r.previous = before
	return summary, nil
}

// AllDefinitions returns discovered agent definitions.
func (r *Registry) AllDefinitions() []Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Definition, len(r.agents))
	copy(out, r.agents)
	return out
}

func userAgentsDir() (string, error) {
	dir, err := config.ResolveSagittariusDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents"), nil
}

// FormatSummary renders a reload summary for slash output.
func FormatSummary(s ReloadSummary) string {
	msg := fmt.Sprintf("Agents reloaded: %d total (%d local, %d remote)", s.TotalLoaded, s.LocalCount, s.RemoteCount)
	if len(s.NewAgents) > 0 {
		msg += fmt.Sprintf("\nNew: %s", joinComma(s.NewAgents))
	}
	if len(s.Errors) > 0 {
		msg += fmt.Sprintf("\nErrors: %d encountered during reload", len(s.Errors))
		for _, e := range s.Errors {
			msg += "\n  - " + e
		}
	}
	return msg
}

func joinComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += ", " + items[i]
	}
	return out
}
