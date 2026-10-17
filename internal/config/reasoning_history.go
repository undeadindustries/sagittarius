package config

import "strings"

// ReasoningHistoryPolicy controls whether assistant reasoning_content is
// replayed on the openai-chat wire and whether older reasoning is pruned
// before context compression.
type ReasoningHistoryPolicy string

const (
	// ReasoningHistoryAuto consults the built-in model-family table to decide
	// whether to replay reasoning_content and prune older reasoning turns.
	ReasoningHistoryAuto ReasoningHistoryPolicy = "auto"
	// ReasoningHistoryKeep replays reasoning_content on the wire and enables
	// batched reasoning retention pruning.
	ReasoningHistoryKeep ReasoningHistoryPolicy = "keep"
	// ReasoningHistoryKeepAll replays reasoning_content on the wire for every
	// turn in history and disables reasoning retention pruning.
	ReasoningHistoryKeepAll ReasoningHistoryPolicy = "keep-all"
	// ReasoningHistoryNone never sends reasoning_content on the wire.
	ReasoningHistoryNone ReasoningHistoryPolicy = "none"
)

// ValidReasoningHistoryPolicies lists the accepted values in cycle order.
var ValidReasoningHistoryPolicies = []string{
	string(ReasoningHistoryAuto),
	string(ReasoningHistoryKeep),
	string(ReasoningHistoryKeepAll),
	string(ReasoningHistoryNone),
}

// IsValidReasoningHistoryPolicy reports whether v is a valid reasoningHistory
// policy value.
func IsValidReasoningHistoryPolicy(v string) bool {
	switch ReasoningHistoryPolicy(strings.ToLower(strings.TrimSpace(v))) {
	case ReasoningHistoryAuto, ReasoningHistoryKeep, ReasoningHistoryKeepAll, ReasoningHistoryNone:
		return true
	default:
		return false
	}
}

// EffectiveReasoningHistoryPolicy returns the configured reasoningHistory
// policy ("auto", "keep", "keep-all", "none") after applying precedence:
//
//	providers.<id>.models.<model>.reasoningHistory
//	  -> providers.<id>.reasoningHistory
//	  -> "auto"
//
// explicit is true when either the per-model or provider-level field was set.
func EffectiveReasoningHistoryPolicy(settings *Settings, providerID, model string) (policy string, explicit bool) {
	if settings != nil {
		if inst := settings.ProviderInstance(providerID); inst != nil {
			if model != "" && inst.Models != nil {
				if mc, ok := inst.Models[model]; ok {
					if p := normalizeReasoningHistoryPolicy(mc.ReasoningHistory); p != "" {
						return p, true
					}
				}
			}
			if p := normalizeReasoningHistoryPolicy(inst.ReasoningHistory); p != "" {
				return p, true
			}
		}
	}
	return string(ReasoningHistoryAuto), false
}

func normalizeReasoningHistoryPolicy(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if IsValidReasoningHistoryPolicy(v) {
		return v
	}
	return ""
}

// ModelReasoningHistoryRule evaluates the built-in model-family table for
// reasoningHistory: "auto". It returns whether reasoning_content should be
// replayed on the openai-chat wire and any family-required chat_template_kwargs
// needed to keep the server's Jinja template from stripping prior thinking.
func ModelReasoningHistoryRule(providerID, baseURL, model string) (wireKeep bool, templateKwargs map[string]any) {
	normProvider := strings.ToLower(strings.TrimSpace(NormalizeProviderID(providerID)))
	normBase := strings.ToLower(strings.TrimSpace(baseURL))
	if normProvider == "openrouter" || strings.Contains(normBase, "openrouter.ai") {
		return false, nil
	}

	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return false, nil
	}
	// Strip optional org/ namespace prefix (e.g. "Qwen/Qwen3.8-32B" -> "qwen3.8-32b")
	// while still checking substrings across the normalized model ID.
	leaf := m
	if idx := strings.LastIndex(m, "/"); idx >= 0 && idx+1 < len(m) {
		leaf = m[idx+1:]
	}

	switch {
	case strings.Contains(leaf, "gpt-oss") || strings.Contains(m, "gpt-oss"):
		return false, nil
	case hasFamilyPrefix(leaf, "qwen3.8", "qwen-3.8"):
		return true, nil
	case hasFamilyPrefix(leaf, "qwen3.6", "qwen-3.6"):
		return true, map[string]any{"preserve_thinking": true}
	case hasFamilyPrefix(leaf, "qwen3", "qwen-3"):
		return true, nil
	case hasFamilyPrefix(leaf, "glm-4.7", "glm4.7"):
		return true, map[string]any{"clear_thinking": false}
	case hasFamilyPrefix(leaf, "glm-4.5", "glm4.5", "glm-4.6", "glm4.6"):
		return true, nil
	case hasFamilyPrefix(leaf, "kimi-k2", "kimi_k2"):
		return true, nil
	case hasFamilyPrefix(leaf, "minimax-m2", "minimax_m2"):
		return true, nil
	case hasFamilyPrefix(leaf, "gemma-4", "gemma4"):
		return true, nil
	case hasFamilyPrefix(leaf, "deepseek"):
		return true, nil
	default:
		return false, nil
	}
}

func hasFamilyPrefix(leaf string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(leaf, p) || strings.Contains(leaf, "-"+p) || strings.Contains(leaf, "_"+p) {
			return true
		}
	}
	return false
}

// ResolveReasoningHistory resolves the wire and retention behavior plus merged
// chat_template_kwargs for (providerID, model).
//
// Return values:
//   - sendOnWire: replay Part.Reasoning as reasoning_content on assistant turns
//   - pruneRetention: run batched reasoning retention pruning before compression
//   - templateKwargs: merged chat_template_kwargs (nil when empty)
func ResolveReasoningHistory(settings *Settings, providerID, model string) (sendOnWire bool, pruneRetention bool, templateKwargs map[string]any) {
	policy, _ := EffectiveReasoningHistoryPolicy(settings, providerID, model)
	baseURL := providerBaseURL(settings, providerID)
	autoKeep, autoKwargs := ModelReasoningHistoryRule(providerID, baseURL, model)

	switch ReasoningHistoryPolicy(policy) {
	case ReasoningHistoryNone:
		sendOnWire = false
		pruneRetention = false
	case ReasoningHistoryKeep:
		sendOnWire = true
		pruneRetention = true
	case ReasoningHistoryKeepAll:
		sendOnWire = true
		pruneRetention = false
	default: // ReasoningHistoryAuto
		sendOnWire = autoKeep
		pruneRetention = autoKeep
	}

	var merged map[string]any
	// Seed auto template kwargs when the policy is "auto" (or when sendOnWire
	// is enabled for a model family that requires template flags).
	if sendOnWire && len(autoKwargs) > 0 {
		merged = make(map[string]any, len(autoKwargs))
		for k, v := range autoKwargs {
			merged[k] = v
		}
	}
	if settings != nil {
		if inst := settings.ProviderInstance(providerID); inst != nil {
			for k, v := range inst.ChatTemplateKwargs {
				if merged == nil {
					merged = make(map[string]any)
				}
				merged[k] = v
			}
			if model != "" && inst.Models != nil {
				if mc, ok := inst.Models[model]; ok {
					for k, v := range mc.ChatTemplateKwargs {
						if merged == nil {
							merged = make(map[string]any)
						}
						merged[k] = v
					}
				}
			}
		}
	}
	return sendOnWire, pruneRetention, merged
}

func providerBaseURL(settings *Settings, providerID string) string {
	if settings == nil || settings.Providers == nil {
		return ""
	}
	if inst := settings.ProviderInstance(providerID); inst != nil && strings.TrimSpace(inst.BaseURL) != "" {
		return inst.BaseURL
	}
	normID := NormalizeProviderID(providerID)
	if settings.Providers.Custom != nil {
		if def, ok := settings.Providers.Custom[normID]; ok {
			return def.BaseURL
		}
	}
	return ""
}
