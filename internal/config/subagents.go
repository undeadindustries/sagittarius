package config

import (
	"fmt"
	"strings"
)

// SubagentClass names an independently-toggled subagent class.
type SubagentClass string

const (
	// SubagentResearch is the read-only research class behind the task tool.
	SubagentResearch SubagentClass = "research"
	// SubagentCoding is the write-capable class behind the code_task tool.
	SubagentCoding SubagentClass = "coding"
	// SubagentReviewer is the read-only review class behind the opt-in
	// post-write reviewer pass.
	SubagentReviewer SubagentClass = "reviewer"
	// SubagentDefault names the fallback slot, not a class.
	SubagentDefault SubagentClass = "default"
)

// SubagentTargetSource names which settings slot produced a SubagentTarget.
type SubagentTargetSource string

const (
	SubagentTargetClass   SubagentTargetSource = "class"
	SubagentTargetDefault SubagentTargetSource = "default"
	SubagentTargetLive    SubagentTargetSource = "live"
)

// SubagentTarget is the resolved (provider, model) pair for a child launch.
// An empty Provider means "the parent's live provider"; a legacy model-only
// pin resolves this way so it keeps working.
type SubagentTarget struct {
	Provider string
	Model    string
	Source   SubagentTargetSource
}

// ResolveReadOnly returns whether the durable read-only inspection mode is enabled.
func ResolveReadOnly(cfg *Settings, fallback bool) bool {
	if cfg == nil || cfg.Sagittarius == nil || cfg.Sagittarius.ReadOnly == nil {
		return fallback
	}
	return *cfg.Sagittarius.ReadOnly
}

// ResearchSubagentsEnabled reports whether read-only research subagents (the
// task tool) are enabled. Project settings win over global. The legacy
// sagittarius.subagents.enabled switch is the fallback so existing
// configurations keep working. The default is false.
func ResearchSubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentClassEnabled(project, SubagentResearch); ok {
		return v
	}
	if v, ok := subagentClassEnabled(global, SubagentResearch); ok {
		return v
	}
	if v, ok := legacySubagentsEnabled(project); ok {
		return v
	}
	if v, ok := legacySubagentsEnabled(global); ok {
		return v
	}
	return false
}

// CodingSubagentsEnabled reports whether write-capable coding subagents (the
// code_task tool) are enabled. Project settings win over global. The legacy
// sagittarius.subagents.enabled switch is deliberately NOT a fallback: it
// predates leased writes and never meant "may modify files". The default is
// false.
func CodingSubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentClassEnabled(project, SubagentCoding); ok {
		return v
	}
	if v, ok := subagentClassEnabled(global, SubagentCoding); ok {
		return v
	}
	return false
}

// ReviewSubagentsEnabled reports whether the opt-in post-write reviewer pass
// runs after a code_task child changes files. Project settings win over
// global. Like coding, it has no legacy fallback and defaults to false: a
// review pass doubles child inference cost and must be chosen deliberately.
func ReviewSubagentsEnabled(global, project *Settings) bool {
	if v, ok := subagentClassEnabled(project, SubagentReviewer); ok {
		return v
	}
	if v, ok := subagentClassEnabled(global, SubagentReviewer); ok {
		return v
	}
	return false
}

// ResolveSubagentTarget resolves the (provider, model) pair for a subagent class.
//
// Resolution order (first non-empty model wins):
//  1. sagittarius.subagents.<class>.{provider,model}
//  2. sagittarius.subagents.default.{provider,model}
//  3. the parent's live (provider, model) pair
//
// Pinning the result keeps a mode override (which may route the parent's own
// mode to an unsuitable engine) from capturing the child. Source reports which
// slot won so launch logging and the UI can say where the pair came from.
func ResolveSubagentTarget(class SubagentClass, cfg *SagittariusSettings, liveProvider, liveModel string) SubagentTarget {
	liveProvider = strings.TrimSpace(liveProvider)
	liveModel = strings.TrimSpace(liveModel)
	if cfg != nil && cfg.Subagents != nil {
		if cls := subagentClass(cfg.Subagents, class); cls != nil {
			if m := strings.TrimSpace(cls.Model); m != "" {
				return SubagentTarget{Provider: strings.TrimSpace(cls.Provider), Model: m, Source: SubagentTargetClass}
			}
		}
		if m := strings.TrimSpace(cfg.Subagents.Default.Model); m != "" {
			return SubagentTarget{Provider: strings.TrimSpace(cfg.Subagents.Default.Provider), Model: m, Source: SubagentTargetDefault}
		}
	}
	return SubagentTarget{Provider: liveProvider, Model: liveModel, Source: SubagentTargetLive}
}

// Subagent routing slots addressable by /subagents. The first four map to the
// subagents.default/research/coding/reviewer blocks; "utility" maps to the
// goal evaluator pair (evaluatorProvider/evaluatorModel), which already drives
// the goal judge, auto-titles, and /memory compact through auxGenerator.
const (
	SubagentSlotDefault  = "default"
	SubagentSlotResearch = "research"
	SubagentSlotCoding   = "coding"
	SubagentSlotReviewer = "reviewer"
	SubagentSlotUtility  = "utility"
)

// SubagentSlots lists every /subagents routing slot in display order.
var SubagentSlots = []string{
	SubagentSlotDefault,
	SubagentSlotResearch,
	SubagentSlotCoding,
	SubagentSlotReviewer,
	SubagentSlotUtility,
}

// ValidSubagentSlot reports whether slot names a /subagents routing slot.
func ValidSubagentSlot(slot string) bool {
	switch slot {
	case SubagentSlotDefault, SubagentSlotResearch, SubagentSlotCoding,
		SubagentSlotReviewer, SubagentSlotUtility:
		return true
	}
	return false
}

// SetSubagentOverride writes a (provider, model) routing pin for slot into s,
// preserving any existing Enabled/Extra. An empty model clears the pin (see
// ClearSubagentOverride). It is the single mutation path shared by the
// headless /subagents command and the subagents dialog.
func SetSubagentOverride(s *Settings, slot, providerID, model string) error {
	if s == nil {
		return fmt.Errorf("set subagent override: nil settings")
	}
	if model == "" {
		ClearSubagentOverride(s, slot)
		return nil
	}
	if s.Sagittarius == nil {
		s.Sagittarius = &SagittariusSettings{}
	}
	switch slot {
	case SubagentSlotUtility:
		if s.Sagittarius.Goal == nil {
			s.Sagittarius.Goal = &SagittariusGoalConfig{}
		}
		s.Sagittarius.Goal.EvaluatorProvider = NormalizeProviderID(providerID)
		s.Sagittarius.Goal.EvaluatorModel = model
		return nil
	case SubagentSlotDefault:
		if s.Sagittarius.Subagents == nil {
			s.Sagittarius.Subagents = &SagittariusSubagents{}
		}
		d := s.Sagittarius.Subagents.Default
		d.Provider = NormalizeProviderID(providerID)
		d.Model = model
		s.Sagittarius.Subagents.Default = d
		return nil
	}
	if s.Sagittarius.Subagents == nil {
		s.Sagittarius.Subagents = &SagittariusSubagents{}
	}
	cls := subagentClass(s.Sagittarius.Subagents, SubagentClass(slot))
	if cls == nil {
		switch SubagentClass(slot) {
		case SubagentResearch:
			cls = &SagittariusSubagentClass{}
			s.Sagittarius.Subagents.Research = cls
		case SubagentCoding:
			cls = &SagittariusSubagentClass{}
			s.Sagittarius.Subagents.Coding = cls
		case SubagentReviewer:
			cls = &SagittariusSubagentClass{}
			s.Sagittarius.Subagents.Reviewer = cls
		default:
			return fmt.Errorf("unknown subagent slot %q (expected default, research, coding, reviewer, utility)", slot)
		}
	}
	cls.Provider = NormalizeProviderID(providerID)
	cls.Model = model
	return nil
}

// ClearSubagentOverride drops the (provider, model) pin for slot, preserving
// any Enabled/Extra (a class slot is removed entirely only when all three are
// empty). It is a no-op for nil settings or an unknown slot.
func ClearSubagentOverride(s *Settings, slot string) {
	if s == nil || s.Sagittarius == nil {
		return
	}
	switch slot {
	case SubagentSlotUtility:
		if s.Sagittarius.Goal == nil {
			return
		}
		s.Sagittarius.Goal.EvaluatorProvider = ""
		s.Sagittarius.Goal.EvaluatorModel = ""
		return
	case SubagentSlotDefault:
		if s.Sagittarius.Subagents == nil {
			return
		}
		s.Sagittarius.Subagents.Default.Provider = ""
		s.Sagittarius.Subagents.Default.Model = ""
		return
	}
	if s.Sagittarius.Subagents == nil {
		return
	}
	cls := subagentClass(s.Sagittarius.Subagents, SubagentClass(slot))
	if cls == nil {
		return
	}
	cls.Provider = ""
	cls.Model = ""
	if cls.Enabled == nil && cls.Extra == nil {
		switch SubagentClass(slot) {
		case SubagentResearch:
			s.Sagittarius.Subagents.Research = nil
		case SubagentCoding:
			s.Sagittarius.Subagents.Coding = nil
		case SubagentReviewer:
			s.Sagittarius.Subagents.Reviewer = nil
		}
	}
}

// ResetSubagentOverrides clears every /subagents routing pin in s (the four
// subagent slots plus the utility pair), preserving enablement switches. It
// reports whether anything changed.
func ResetSubagentOverrides(s *Settings) bool {
	if s == nil || s.Sagittarius == nil {
		return false
	}
	changed := false
	for _, slot := range SubagentSlots {
		if subagentSlotPinned(s, slot) {
			ClearSubagentOverride(s, slot)
			changed = true
		}
	}
	return changed
}

func subagentSlotPinned(s *Settings, slot string) bool {
	if s == nil || s.Sagittarius == nil {
		return false
	}
	switch slot {
	case SubagentSlotUtility:
		return s.Sagittarius.Goal != nil && s.Sagittarius.Goal.EvaluatorModel != ""
	case SubagentSlotDefault:
		return s.Sagittarius.Subagents != nil && s.Sagittarius.Subagents.Default.Model != ""
	}
	if s.Sagittarius.Subagents == nil {
		return false
	}
	cls := subagentClass(s.Sagittarius.Subagents, SubagentClass(slot))
	return cls != nil && cls.Model != ""
}

// DefaultSubagentMaxAttempts caps delegations of the same task per session
// when sagittarius.subagents.maxAttempts is unset.
const DefaultSubagentMaxAttempts = 2

// ResolveSubagentMaxAttempts returns the effective delegation cap for one task
// identity. Nil falls back to def (the compiled-in 2). 0 means no cap —
// mirroring ResolveMaxToolRounds. A negative pin is treated as unset so a
// corrupt value cannot disable delegation; ValidateSagittariusSettings rejects
// negatives on load.
func ResolveSubagentMaxAttempts(s *SagittariusSettings, def int) int {
	if s == nil || s.Subagents == nil || s.Subagents.MaxAttempts == nil {
		return def
	}
	if n := *s.Subagents.MaxAttempts; n >= 0 {
		return n
	}
	return def
}

// SubagentMaxConcurrentRange bounds sagittarius.subagents.maxConcurrent.
const (
	MinSubagentMaxConcurrent = 1
	MaxSubagentMaxConcurrent = 16
)

// ResolveSubagentMaxConcurrent returns the effective subagent fan-out cap.
// Nil falls back to def (the compiled-in 8). Out-of-range pins are treated as
// unset so a corrupt value cannot serialize or explode fan-out;
// ValidateSagittariusSettings rejects them on load.
func ResolveSubagentMaxConcurrent(s *SagittariusSettings, def int) int {
	if s == nil || s.Subagents == nil || s.Subagents.MaxConcurrent == nil {
		return def
	}
	if n := *s.Subagents.MaxConcurrent; n >= MinSubagentMaxConcurrent && n <= MaxSubagentMaxConcurrent {
		return n
	}
	return def
}

// SubagentModel selects the model for a subagent class.
//
// Resolution order (first non-empty wins):
//  1. sagittarius.subagents.<class>.model
//  2. sagittarius.subagents.default.model
//  3. liveModel — the model the parent loop is currently using
//
// Pinning the result keeps a mode override (which may route the parent's own
// mode to an unsuitable engine) from capturing the child.
func SubagentModel(class SubagentClass, cfg *SagittariusSettings, liveModel string) string {
	return ResolveSubagentTarget(class, cfg, "", liveModel).Model
}

func subagentClass(s *SagittariusSubagents, class SubagentClass) *SagittariusSubagentClass {
	if s == nil {
		return nil
	}
	switch class {
	case SubagentResearch:
		return s.Research
	case SubagentCoding:
		return s.Coding
	case SubagentReviewer:
		return s.Reviewer
	}
	return nil
}

func subagentClassEnabled(s *Settings, class SubagentClass) (bool, bool) {
	if s == nil || s.Sagittarius == nil {
		return false, false
	}
	cls := subagentClass(s.Sagittarius.Subagents, class)
	if cls == nil || cls.Enabled == nil {
		return false, false
	}
	return *cls.Enabled, true
}

func legacySubagentsEnabled(s *Settings) (bool, bool) {
	if s == nil || s.Sagittarius == nil || s.Sagittarius.Subagents == nil {
		return false, false
	}
	if v := s.Sagittarius.Subagents.Enabled; v != nil {
		return *v, true
	}
	return false, false
}
