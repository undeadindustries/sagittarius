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
				Description: "Clear the session reasoning override (does not touch settings)",
				Handler:     handleReasoningClear,
			},
			{
				Name:        "adaptive",
				Description: "Alias for 'clear' — return to the adaptive/capability-aware default",
				Handler:     handleReasoningClear,
			},
			{
				Name:        "save",
				Description: "Persist <level> to providers.<active>.reasoningEffort",
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
		Description: fmt.Sprintf("Set session reasoning effort to '%s' (not persisted)", name),
		Handler: func(ctx *Context) Result {
			return handleReasoningSetLevel(ctx, name)
		},
	}
}

// reasoningContext resolves the shared state every /reasoning sub-command
// needs: the active provider summary and the live mode-resolved model.
func reasoningContext(ctx *Context) (provider.EffectiveProviderSummary, string, error) {
	eff, err := provider.EffectiveProvider(ctx.Deps.Settings)
	if err != nil {
		return provider.EffectiveProviderSummary{}, "", err
	}
	_, model := ctx.Deps.Hooks.InteractionMode()
	return eff, model, nil
}

func handleReasoningShow(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, model, err := reasoningContext(ctx)
	if err != nil {
		return ErrorResult(err)
	}
	override := ctx.Deps.Hooks.ReasoningOverride()
	persisted := eff.ReasoningEffort
	resolution := config.ResolveReasoningRequest(ctx.Deps.Settings, eff.ProviderID, model, override)
	profile, matched := config.ModelReasoningRule(eff.WireFormat, model)

	lines := []string{
		fmt.Sprintf("Active provider: %s (%s), model: %s", eff.ProviderID, eff.DisplayName, model),
	}

	switch {
	case resolution == nil:
		lines = append(lines, "Resolved reasoning: off (server/provider default) — no override, pin, or known reasoning capability for this model.")
	case resolution.Effort != "":
		source := fmt.Sprintf("provider default (providers.%s.reasoningEffort)", eff.ProviderID)
		if override != "" {
			source = "session override (set via /reasoning <level>)"
		}
		lines = append(lines, fmt.Sprintf("Resolved reasoning effort: %s — %s", resolution.Effort, source))
	case matched && profile.Mechanism == config.ReasoningMechanismGeminiDynamic:
		lines = append(lines, "Resolved reasoning: adaptive (dynamic thinking) — Gemini decides depth per turn. Pin a level with /reasoning <level> or disable with /reasoning none.")
	default:
		lines = append(lines, "Resolved reasoning: on, provider default effort (discovered reasoning-capable model).")
	}

	if override != "" && persisted != "" && override != persisted {
		lines = append(lines, fmt.Sprintf("  Persisted value: %s (clear session override with /reasoning clear)", persisted))
	}
	if matched && len(profile.ValidEfforts) > 0 {
		extra := "Valid levels: " + strings.Join(profile.ValidEfforts, ", ")
		if profile.Mandatory {
			extra += " (mandatory — cannot be disabled)"
		}
		lines = append(lines, extra)
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleReasoningClear(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, _, err := reasoningContext(ctx)
	if err != nil {
		return ErrorResult(err)
	}

	had := ctx.Deps.Hooks.ReasoningOverride()
	ctx.Deps.Hooks.ClearReasoningOverride()
	if had == "" {
		return InfoResult(fmt.Sprintf(
			"No session reasoning override was set; nothing to clear. Falling back to %s.",
			defaultOrServer(eff.ReasoningEffort),
		))
	}
	return InfoResult(fmt.Sprintf(
		"Session reasoning override cleared. Falling back to %s.",
		defaultOrServer(eff.ReasoningEffort),
	))
}

func handleReasoningSave(ctx *Context) Result {
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, model, err := reasoningContext(ctx)
	if err != nil {
		return ErrorResult(err)
	}

	level := strings.TrimSpace(ctx.Args)
	if level == "" {
		return InfoResult("Usage: /reasoning save <level>  (see /reasoning show for valid levels)")
	}
	if verr := validateReasoningLevel(eff.WireFormat, model, level); verr != nil {
		return InfoResult(verr.Error())
	}
	if err := provider.SetProviderReasoningEffort(ctx.Deps.Settings, eff.ProviderID, level); err != nil {
		return ErrorResult(err)
	}
	if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
		return ErrorResult(fmt.Errorf("persist reasoning effort: %w", err))
	}
	ctx.Deps.Hooks.ClearReasoningOverride()
	if _, _, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx); err != nil {
		return ErrorResult(fmt.Errorf("rebuild runner after reasoning save: %w", err))
	}
	return InfoResult(fmt.Sprintf(
		"Saved providers.%s.reasoningEffort = %s. Live on the next request — no restart needed.",
		eff.ProviderID,
		level,
	))
}

func handleReasoningSetLevel(ctx *Context, level string) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, model, err := reasoningContext(ctx)
	if err != nil {
		return ErrorResult(err)
	}
	if verr := validateReasoningLevel(eff.WireFormat, model, level); verr != nil {
		return InfoResult(verr.Error())
	}
	ctx.Deps.Hooks.SetReasoningOverride(level)
	return InfoResult(fmt.Sprintf(
		"Session reasoning override set to '%s'. Persist it with /reasoning save %s or drop it with /reasoning clear.",
		level,
		level,
	))
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

// validateReasoningLevel rejects a level against the model's known profile
// when one exists (ModelReasoningRule matched), including a Mandatory
// rejection of "none"/"off"; otherwise it falls back to the generic
// cross-family level set (provider.IsValidReasoningLevel) since no per-model
// capability is known.
func validateReasoningLevel(wireFormat config.WireFormat, model, level string) error {
	level = strings.TrimSpace(level)
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
