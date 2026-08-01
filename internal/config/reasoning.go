package config

import (
	"fmt"
	"strings"
)

// ReasoningMechanism identifies how a resolved reasoning request should be
// realized on the wire — which field(s) an adapter needs to set. It is a
// property of the model family / wire format, not of any single request.
type ReasoningMechanism string

const (
	// ReasoningMechanismNone means the model has no known reasoning control.
	ReasoningMechanismNone ReasoningMechanism = ""
	// ReasoningMechanismFixedEffort covers OpenAI Responses reasoning models
	// (gpt-5 family, o3/o4): a discrete effort level, no true dynamic budget.
	ReasoningMechanismFixedEffort ReasoningMechanism = "fixed-effort"
	// ReasoningMechanismGeminiDynamic covers Gemini 3 / 2.5: genuine
	// provider-native adaptive thinking via ThinkingBudget=-1, with an
	// optional fixed ThinkingLevel pin (Gemini 3 only).
	ReasoningMechanismGeminiDynamic ReasoningMechanism = "gemini-dynamic"
	// ReasoningMechanismOpenRouter covers openai-chat models whose capability
	// was discovered at runtime via OpenRouter's per-model `reasoning` object
	// (GET /v1/models), cached into ProviderModelConfig.ReasoningSupported.
	ReasoningMechanismOpenRouter ReasoningMechanism = "openrouter"
)

// ReasoningProfile describes what a model family supports for reasoning.
type ReasoningProfile struct {
	Mechanism ReasoningMechanism
	// ValidEfforts lists the accepted effort strings for this family, in
	// ascending order. Empty for mechanisms without discrete effort levels
	// (pure dynamic Gemini thinking has none pinned by default).
	ValidEfforts []string
	// DefaultEffort is the family's own default when reasoning is enabled but
	// no effort is pinned. Empty means "let the provider apply its own
	// default" (the adapter omits the field rather than guessing).
	DefaultEffort string
	// SupportsDynamic is true for mechanisms with a genuine adaptive/dynamic
	// mode (Gemini's ThinkingBudget=-1), as opposed to only discrete levels.
	SupportsDynamic bool
	// Mandatory is true when reasoning cannot be disabled for this family
	// (e.g. gpt-5-pro is always "high").
	Mandatory bool
}

// ModelReasoningRule reports a static, model-family reasoning capability
// opinion, mirroring ModelTemperatureRule. matched=false means "no static
// opinion" — for openai-chat models that means the caller should consult the
// discovered OpenRouter capability cache instead (see ResolveReasoningRequest).
//
// This table will go stale as new model families ship; update it alongside
// ModelTemperatureRule when a new reasoning-capable family launches.
func ModelReasoningRule(wireFormat WireFormat, model string) (profile ReasoningProfile, matched bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return ReasoningProfile{}, false
	}
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}

	switch wireFormat {
	case WireFormatGemini:
		if strings.Contains(m, "gemini-3") || strings.Contains(m, "gemini-2.5") {
			return ReasoningProfile{
				Mechanism:       ReasoningMechanismGeminiDynamic,
				ValidEfforts:    []string{"minimal", "low", "medium", "high"},
				SupportsDynamic: true,
			}, true
		}
		return ReasoningProfile{}, false
	case WireFormatOpenAIResponses:
		switch {
		case strings.HasPrefix(m, "gpt-5-pro"):
			return ReasoningProfile{
				Mechanism:     ReasoningMechanismFixedEffort,
				ValidEfforts:  []string{"high"},
				DefaultEffort: "high",
				Mandatory:     true,
			}, true
		case strings.HasPrefix(m, "gpt-5.4"), strings.HasPrefix(m, "gpt-5.5"), strings.HasPrefix(m, "gpt-5.6"):
			return ReasoningProfile{
				Mechanism:     ReasoningMechanismFixedEffort,
				ValidEfforts:  []string{"none", "low", "medium", "high", "xhigh"},
				DefaultEffort: "medium",
			}, true
		case strings.HasPrefix(m, "gpt-5.1"), strings.HasPrefix(m, "gpt-5.2"), strings.HasPrefix(m, "gpt-5.3"):
			return ReasoningProfile{
				Mechanism:     ReasoningMechanismFixedEffort,
				ValidEfforts:  []string{"none", "low", "medium", "high"},
				DefaultEffort: "none",
			}, true
		case strings.HasPrefix(m, "gpt-5"):
			return ReasoningProfile{
				Mechanism:     ReasoningMechanismFixedEffort,
				ValidEfforts:  []string{"minimal", "low", "medium", "high"},
				DefaultEffort: "medium",
			}, true
		case strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
			return ReasoningProfile{
				Mechanism:     ReasoningMechanismFixedEffort,
				ValidEfforts:  []string{"low", "medium", "high"},
				DefaultEffort: "medium",
			}, true
		default:
			return ReasoningProfile{}, false
		}
	default:
		return ReasoningProfile{}, false
	}
}

// ReasoningResolution is the resolved reasoning request for one round,
// expressed without any dependency on the provider package (config is a leaf
// that provider imports, so it must not import provider back). Callers with
// access to provider.ReasoningRequest (internal/agent.Runner) translate this
// 1:1 when building the neutral GenerateRequest.
type ReasoningResolution struct {
	Effort  string
	Enabled bool
}

// resolveWireFormat looks up the wire format for a provider id, consulting
// its custom definition (if any) so presets and user-defined providers
// resolve the same way provider.ResolveEndpointConfig does.
func resolveWireFormat(settings *Settings, providerID string) WireFormat {
	if settings == nil {
		return ""
	}
	var custom *CustomProviderDefinition
	if settings.Providers != nil {
		if c, ok := settings.Providers.Custom[NormalizeProviderID(providerID)]; ok {
			custom = &c
		}
	}
	return ProviderWireFormat(providerID, custom)
}

// ResolveReasoningRequest resolves what reasoning request (if any) should be
// sent for this round. sessionOverride is an ephemeral, non-persisted pin
// (owned by the caller, e.g. Runner's per-model /reasoning override) that
// takes precedence over everything else; pass "" when none is set.
//
// Resolution order:
//  1. sessionOverride (ephemeral, explicit)
//  2. pinned providers.<id>.models.<model>.reasoningEffort /
//     providers.<id>.reasoningEffort (persisted, explicit)
//  3. discovered OpenRouter per-model capability (default_enabled=true)
//  4. static model-family default (Gemini dynamic thinking; OpenAI Responses
//     families return nil here so the provider's own default applies — see
//     below)
//  5. nil (send nothing; current default behavior for unknown models)
//
// A nil result is not an error: it means the adapter should omit the
// reasoning field entirely, matching today's behavior for models with no
// known reasoning capability.
func ResolveReasoningRequest(settings *Settings, providerID, model, sessionOverride string) *ReasoningResolution {
	if settings == nil {
		return nil
	}

	if override := strings.TrimSpace(sessionOverride); override != "" {
		return &ReasoningResolution{Effort: override, Enabled: true}
	}

	inst := settings.ProviderInstance(providerID)
	pinned := ""
	var reasoningSupported *bool
	if inst != nil {
		if mc, ok := lookupModelConfig(inst, model); ok {
			pinned = strings.TrimSpace(mc.ReasoningEffort)
			reasoningSupported = mc.ReasoningSupported
		}
		if pinned == "" {
			pinned = strings.TrimSpace(inst.ReasoningEffort)
		}
	}
	if pinned != "" {
		return &ReasoningResolution{Effort: pinned, Enabled: true}
	}

	wireFormat := resolveWireFormat(settings, providerID)
	if profile, matched := ModelReasoningRule(wireFormat, model); matched {
		switch profile.Mechanism {
		case ReasoningMechanismGeminiDynamic:
			// Dynamic/adaptive by default: empty effort tells the Gemini
			// adapter to request ThinkingBudget=-1 rather than a fixed level.
			return &ReasoningResolution{Enabled: true}
		case ReasoningMechanismFixedEffort:
			// Let the provider apply its own family default; omitting the
			// field entirely matches today's behavior.
			return nil
		}
	}

	if wireFormat == WireFormatOpenAIChat && reasoningSupported != nil && *reasoningSupported {
		return &ReasoningResolution{Enabled: true}
	}

	return nil
}

// DescribeReasoningCapability renders a short, human-readable, one-line
// description of a model's resolved reasoning capability for read-only
// display in settings UIs (the /models dialog capability hint, /reasoning
// show's summary line). It never pins or persists anything — purely
// descriptive, sourced from the same ModelReasoningRule / discovered-cache
// data ResolveReasoningRequest uses.
func DescribeReasoningCapability(settings *Settings, providerID, model string) string {
	wireFormat := resolveWireFormat(settings, providerID)
	if profile, matched := ModelReasoningRule(wireFormat, model); matched {
		switch profile.Mechanism {
		case ReasoningMechanismGeminiDynamic:
			return "adaptive (dynamic thinking) — decides depth per turn; pin a level or disable with 'none'"
		case ReasoningMechanismFixedEffort:
			levels := strings.Join(profile.ValidEfforts, ", ")
			if profile.Mandatory {
				return fmt.Sprintf("fixed effort, mandatory — %s", levels)
			}
			return fmt.Sprintf("fixed effort, adaptive by default — %s", levels)
		}
	}

	if wireFormat == WireFormatOpenAIChat {
		if inst := settings.ProviderInstance(providerID); inst != nil {
			if mc, ok := lookupModelConfig(inst, model); ok && mc.ReasoningSupported != nil {
				if *mc.ReasoningSupported {
					return "adaptive by default — enabled (discovered reasoning-capable model)"
				}
				return "not offered for this model (discovered)"
			}
		}
	}

	return "not supported / unknown for this model"
}
