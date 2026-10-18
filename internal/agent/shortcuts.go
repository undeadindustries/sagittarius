package agent

import (
	"fmt"
	"slices"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// reasoningOff is the level that disables reasoning on every wire format
// that supports turning it off (see /reasoning none).
const reasoningOff = "none"

// genericReasoningCycle is offered when nothing is known about the model's
// reasoning levels, matching the levels every adapter understands.
var genericReasoningCycle = []string{"", reasoningOff, "low", "medium", "high"}

// reasoningCycle lists the session overrides Alt+R steps through for one
// model. The first entry is always "" (no override: the saved setting or the
// model's own default). A mandatory model never offers "none". A model that
// discovery reported as not reasoning-capable has nothing to cycle, so it
// returns nil.
func reasoningCycle(efforts []string, mandatory, known bool) []string {
	if !known {
		return slices.Clone(genericReasoningCycle)
	}
	if len(efforts) == 0 {
		return nil
	}
	cycle := []string{""}
	hasOff := slices.Contains(efforts, reasoningOff) || slices.Contains(efforts, "off")
	if !mandatory && !hasOff {
		cycle = append(cycle, reasoningOff)
	}
	for _, e := range efforts {
		if mandatory && (e == reasoningOff || e == "off") {
			continue
		}
		cycle = append(cycle, e)
	}
	return cycle
}

// nextInCycle returns the entry after current, wrapping. An unknown current
// value (set through /reasoning to a level outside the cycle) is treated as
// the first entry, so the next press lands on the first real level.
func nextInCycle(cycle []string, current string) string {
	idx := max(slices.Index(cycle, current), 0)
	return cycle[wrapIndex(idx, 1, len(cycle))]
}

// liveReasoningTarget returns the settings view, active provider, and live
// mode-resolved model that the runner's reasoning override is keyed on.
func (a *App) liveReasoningTarget() (*config.Settings, string, string, error) {
	if a.runner == nil {
		return nil, "", "", fmt.Errorf("runner not available")
	}
	s := a.effectiveSettings()
	if s == nil {
		return nil, "", "", fmt.Errorf("settings not loaded")
	}
	model := strings.TrimSpace(a.runner.Model())
	if model == "" {
		return nil, "", "", fmt.Errorf("no active model")
	}
	return s, config.NormalizeProviderID(s.ActiveProvider()), model, nil
}

// CycleReasoningEffort implements ui.ReasoningController (Alt+R). It steps the
// session-only reasoning override through the levels the live model accepts.
// Settings are never written; /reasoning save persists a level.
func (a *App) CycleReasoningEffort() (level, desc string, err error) {
	s, providerID, model, err := a.liveReasoningTarget()
	if err != nil {
		return "", "", err
	}
	efforts, _, mandatory, known := config.ModelReasoningOptions(s, providerID, model)
	cycle := reasoningCycle(efforts, mandatory, known)
	if len(cycle) < 2 {
		return "", "", fmt.Errorf("%s has no reasoning levels to switch between", model)
	}
	next := nextInCycle(cycle, a.runner.ReasoningOverride())
	if next == "" {
		a.runner.ClearReasoningOverride()
	} else {
		a.runner.SetReasoningOverride(next)
	}
	return next, describeReasoningStep(s, providerID, model, next), nil
}

// describeReasoningStep is the transcript line for one Alt+R press. Clearing
// the override names what takes over, because "default" alone would hide a
// level pinned in settings.
func describeReasoningStep(s *config.Settings, providerID, model, level string) string {
	if level != "" {
		return fmt.Sprintf("Reasoning effort → %s for %s this session (Alt+R).", level, model)
	}
	if res := config.ResolveReasoningRequest(s, providerID, model, ""); res != nil && res.Effort != "" {
		return fmt.Sprintf("Reasoning effort → saved setting (%s) for %s (Alt+R).", res.Effort, model)
	}
	return fmt.Sprintf("Reasoning effort → model default for %s (Alt+R).", model)
}

// liveReasoningEffort is the explicit effort the next request will carry for
// the footer: the session override, else a level pinned in settings. Empty
// when the model or provider picks the depth itself.
func (a *App) liveReasoningEffort(s *config.Settings) string {
	if a.runner == nil || s == nil {
		return ""
	}
	model := strings.TrimSpace(a.runner.Model())
	if model == "" {
		return ""
	}
	providerID := config.NormalizeProviderID(s.ActiveProvider())
	res := config.ResolveReasoningRequest(s, providerID, model, a.runner.ReasoningOverride())
	if res == nil {
		return ""
	}
	return res.Effort
}

// ToggleYolo implements ui.ApprovalController (Alt+Y): yolo on or off for this
// session only. The mode gate, project boundary, and read-only posture still
// apply in yolo; only the confirmation prompt is skipped.
func (a *App) ToggleYolo() (mode string, yolo bool, err error) {
	if a.runner == nil {
		return "", false, fmt.Errorf("runner not available")
	}
	m, on := a.runner.ToggleYolo()
	return string(m), on, nil
}
