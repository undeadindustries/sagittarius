package slash

import (
	"fmt"
	"slices"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func reasoningCommand() Command {
	levels := make([]Command, 0, len(provider.ValidReasoningLevels))
	for _, level := range provider.ValidReasoningLevels {
		levels = append(levels, reasoningLevelCommand(level))
	}
	return Command{
		Name:        "reasoning",
		Description: "Show or override the reasoning effort/thinking for the active model",
		SubCommands: append([]Command{
			{
				Name:        "show",
				Description: "Show resolved reasoning mechanism, effort, and its source",
				Handler:     handleReasoningShow,
			},
			{
				Name:        "clear",
				Description: "Clear the saved per-model reasoning effort pin",
				Handler:     handleReasoningClear,
			},
			{
				Name:        "adaptive",
				Description: "Alias for 'clear' — return to the adaptive/capability-aware default",
				Handler:     handleReasoningClear,
			},
			{
				Name:        "save",
				Description: "Alias for /reasoning <level> — save providers.<active>.models.<model>.reasoningEffort",
				Handler:     handleReasoningSave,
			},
		}, levels...),
		Handler: handleReasoningRoot,
	}
}

func reasoningLevelCommand(level provider.ReasoningEffortLevel) Command {
	name := string(level)
	return Command{
		Name:        name,
		Description: fmt.Sprintf("Set and remember reasoning effort '%s' for the active model", name),
		Handler: func(ctx *Context) Result {
			return handleReasoningSetLevel(ctx, name)
		},
	}
}

// reasoningContext resolves the shared state every /reasoning sub-command
// needs: the live settings (merged + runtime mode provider), active provider
// summary, and live mode-resolved model.
func reasoningContext(ctx *Context) (*config.Settings, provider.EffectiveProviderSummary, string, error) {
	s := ctx.Deps.Settings
	if ctx.Deps.Hooks != nil {
		if live := ctx.Deps.Hooks.EffectiveSettings(); live != nil {
			s = live
		}
	}
	if s == nil {
		return nil, provider.EffectiveProviderSummary{}, "", fmt.Errorf("settings not loaded")
	}
	eff, err := provider.EffectiveProvider(s)
	if err != nil {
		return nil, provider.EffectiveProviderSummary{}, "", err
	}
	model := ""
	if ctx.Deps.Hooks != nil {
		_, model = ctx.Deps.Hooks.InteractionMode()
	}
	if model == "" {
		if ep, epErr := provider.ResolveEndpointConfig(s); epErr == nil {
			model = ep.Model
		}
	}
	return s, eff, model, nil
}

func handleReasoningShow(ctx *Context) Result {
	if ctx.Deps.Settings == nil && (ctx.Deps.Hooks == nil || ctx.Deps.Hooks.EffectiveSettings() == nil) {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	s, eff, model, err := reasoningContext(ctx)
	if err != nil {
		return ErrorResult(err)
	}
	modelPin := ""
	if inst := s.ProviderInstance(eff.ProviderID); inst != nil {
		if mc, ok := config.LookupModelConfig(inst, model); ok {
			modelPin = strings.TrimSpace(mc.ReasoningEffort)
		}
	}
	resolution := config.ResolveReasoningRequest(s, eff.ProviderID, model, "")
	profile, matched := config.ModelReasoningRule(eff.WireFormat, model)

	lines := []string{
		fmt.Sprintf("Active provider: %s (%s), model: %s", eff.ProviderID, eff.DisplayName, model),
	}

	switch {
	case resolution == nil:
		lines = append(lines, "Resolved reasoning: off (server/provider default) — no pin or known reasoning capability for this model.")
	case resolution.Effort != "":
		source := fmt.Sprintf("provider default (providers.%s.reasoningEffort)", eff.ProviderID)
		if modelPin != "" {
			source = fmt.Sprintf("saved per-model setting (providers.%s.models.%s.reasoningEffort)", eff.ProviderID, model)
		}
		lines = append(lines, fmt.Sprintf("Resolved reasoning effort: %s — %s", resolution.Effort, source))
	case matched && profile.Mechanism == config.ReasoningMechanismGeminiDynamic:
		lines = append(lines, "Resolved reasoning: adaptive (dynamic thinking) — Gemini decides depth per turn. Pin a level with /reasoning <level> or disable with /reasoning none.")
	case matched && profile.Mechanism == config.ReasoningMechanismAnthropic:
		lines = append(lines, "Resolved reasoning: adaptive thinking — Claude decides depth per turn. Pin a level with /reasoning <level>.")
	default:
		lines = append(lines, "Resolved reasoning: on, provider default effort (discovered reasoning-capable model).")
	}

	efforts, defaultEffort, mandatory, known := config.ModelReasoningOptions(s, eff.ProviderID, model)
	if known && len(efforts) > 0 {
		extra := "Valid levels: " + strings.Join(efforts, ", ")
		if defaultEffort != "" {
			extra += " (model default: " + defaultEffort + ")"
		}
		if mandatory {
			extra += " (mandatory — cannot be disabled)"
		}
		lines = append(lines, extra)
	} else if matched && len(profile.ValidEfforts) > 0 {
		extra := "Valid levels: " + strings.Join(profile.ValidEfforts, ", ")
		if profile.Mandatory {
			extra += " (mandatory — cannot be disabled)"
		}
		lines = append(lines, extra)
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleReasoningClear(ctx *Context) Result {
	s, eff, model, err := reasoningContext(ctx)
	if err != nil {
		if ctx.Deps.Settings == nil {
			return InfoResult("Reasoning commands unavailable: settings not loaded.")
		}
		return ErrorResult(err)
	}

	had := ""
	if inst := s.ProviderInstance(eff.ProviderID); inst != nil {
		if mc, ok := config.LookupModelConfig(inst, model); ok {
			had = strings.TrimSpace(mc.ReasoningEffort)
		}
	}
	if err := saveReasoningLevel(ctx, eff.ProviderID, model, ""); err != nil {
		return ErrorResult(err)
	}
	if had == "" {
		return InfoResult(fmt.Sprintf(
			"No per-model reasoning pin was set for %s. Falling back to %s.",
			model,
			defaultOrServer(eff.ReasoningEffort),
		))
	}
	return InfoResult(fmt.Sprintf(
		"Cleared reasoning effort for %s. Falling back to %s.",
		model,
		defaultOrServer(eff.ReasoningEffort),
	))
}

func handleReasoningSave(ctx *Context) Result {
	level := strings.TrimSpace(ctx.Args)
	if level == "" {
		return InfoResult("Usage: /reasoning save <level>  (see /reasoning show for valid levels)")
	}
	return handleReasoningSetLevel(ctx, level)
}

func handleReasoningSetLevel(ctx *Context, level string) Result {
	s, eff, model, err := reasoningContext(ctx)
	if err != nil {
		if ctx.Deps.Settings == nil {
			return InfoResult("Reasoning commands unavailable: settings not loaded.")
		}
		return ErrorResult(err)
	}
	if verr := validateReasoningLevel(s, eff.WireFormat, eff.ProviderID, model, level); verr != nil {
		return InfoResult(verr.Error())
	}
	if err := saveReasoningLevel(ctx, eff.ProviderID, model, level); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf(
		"Saved providers.%s.models.%s.reasoningEffort = %s. Live on the next request — no restart needed.",
		eff.ProviderID,
		model,
		level,
	))
}

func saveReasoningLevel(ctx *Context, providerID, model, level string) error {
	if ctx.Deps.Hooks != nil {
		return ctx.Deps.Hooks.SaveModelReasoningEffort(providerID, model, level)
	}
	if ctx.Deps.Settings == nil || ctx.Deps.Loader == nil {
		return fmt.Errorf("settings not loaded")
	}
	if level == "" {
		if err := provider.ClearModelConfig(ctx.Deps.Settings, providerID, model, "reasoningEffort"); err != nil {
			return err
		}
	} else {
		if err := provider.SetModelReasoningEffort(ctx.Deps.Settings, providerID, model, level); err != nil {
			return err
		}
	}
	return ctx.Deps.Loader.Save(ctx.Deps.Settings)
}

func handleReasoningRoot(ctx *Context) Result {
	args := strings.TrimSpace(ctx.Args)
	if args == "" {
		return handleReasoningShow(ctx)
	}
	parts := strings.Fields(args)
	head := parts[0]
	if provider.IsValidReasoningLevel(head) {
		return handleReasoningSetLevel(ctx, head)
	}
	switch head {
	case "show":
		return handleReasoningShow(ctx)
	case "clear", "adaptive":
		return handleReasoningClear(ctx)
	case "save":
		saveCtx := *ctx
		saveCtx.Args = strings.TrimSpace(strings.TrimPrefix(args, "save"))
		return handleReasoningSave(&saveCtx)
	default:
		return InfoResult("Unknown sub-command '" + head + "'. Expected: show, clear, adaptive, save <level>, or a reasoning level (see /reasoning show).")
	}
}

// validateReasoningLevel rejects a level against the model's known options
// when ModelReasoningOptions reports known=true with a non-empty list;
// otherwise it falls back to the generic cross-family level set.
func validateReasoningLevel(settings *config.Settings, wireFormat config.WireFormat, providerID, model, level string) error {
	level = strings.TrimSpace(level)
	efforts, _, mandatory, known := config.ModelReasoningOptions(settings, providerID, model)
	if known {
		if level == "none" || level == "off" {
			if mandatory {
				return fmt.Errorf("reasoning is mandatory for %s and cannot be disabled", model)
			}
			return nil
		}
		if len(efforts) > 0 && !slices.Contains(efforts, level) {
			return fmt.Errorf("unknown reasoning level %q for %s. Expected one of: %s", level, model, strings.Join(efforts, ", "))
		}
		if len(efforts) > 0 || mandatory {
			return nil
		}
	}
	// Fall back to static family rule when options were unknown or empty.
	if profile, matched := config.ModelReasoningRule(wireFormat, model); matched {
		if profile.Mandatory && (level == "none" || level == "off") {
			return fmt.Errorf("reasoning is mandatory for %s and cannot be disabled", model)
		}
		if len(profile.ValidEfforts) > 0 && !slices.Contains(profile.ValidEfforts, level) {
			return fmt.Errorf("unknown reasoning level %q for %s. Expected one of: %s", level, model, strings.Join(profile.ValidEfforts, ", "))
		}
		return nil
	}
	if !provider.IsValidReasoningLevel(level) {
		return fmt.Errorf("unknown reasoning level %q. Expected one of: %s", level, strings.Join(reasoningLevelStrings(), ", "))
	}
	return nil
}

func reasoningLevelStrings() []string {
	out := make([]string, 0, len(provider.ValidReasoningLevels))
	for _, l := range provider.ValidReasoningLevels {
		out = append(out, string(l))
	}
	return out
}

func defaultOrServer(value string) string {
	if value == "" {
		return "(server default)"
	}
	return value
}
