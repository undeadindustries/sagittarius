package slash

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func reasoningCommand() Command {
	levels := []Command{
		reasoningLevelCommand(provider.ReasoningMinimal),
		reasoningLevelCommand(provider.ReasoningLow),
		reasoningLevelCommand(provider.ReasoningMedium),
		reasoningLevelCommand(provider.ReasoningHigh),
	}
	return Command{
		Name:        "reasoning",
		Description: "Show or override reasoning effort for OpenAI Responses providers",
		SubCommands: append([]Command{
			{
				Name:        "show",
				Description: "Show resolved reasoning effort and its source",
				Handler:     handleReasoningShow,
			},
			{
				Name:        "clear",
				Description: "Clear the session reasoning override (does not touch settings)",
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

func handleReasoningShow(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, err := provider.EffectiveProvider(ctx.Deps.Settings)
	if err != nil {
		return ErrorResult(err)
	}
	if eff.WireFormat != config.WireFormatOpenAIResponses {
		return reasoningNotApplicable(eff.WireFormat)
	}

	session := provider.SessionReasoningOverride()
	persisted := eff.ReasoningEffort
	resolved := provider.ResolveReasoningEffort(persisted)

	lines := []string{
		fmt.Sprintf("Active provider: %s (%s)", eff.ProviderID, eff.DisplayName),
	}
	if resolved != "" {
		source := fmt.Sprintf("provider default (providers.%s.reasoningEffort)", eff.ProviderID)
		if session != "" {
			source = "session override (set via /reasoning <level>)"
		}
		lines = append(lines, fmt.Sprintf("Resolved reasoning effort: %s — %s", resolved, source))
	} else {
		lines = append(lines, "Resolved reasoning effort: (server default) — no session override or provider setting")
	}
	if session != "" && persisted != "" && session != persisted {
		lines = append(lines, fmt.Sprintf("  Persistent value: %s (clear session override with /reasoning clear)", persisted))
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleReasoningClear(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, err := provider.EffectiveProvider(ctx.Deps.Settings)
	if err != nil {
		return ErrorResult(err)
	}
	if eff.WireFormat != config.WireFormatOpenAIResponses {
		return reasoningNotApplicable(eff.WireFormat)
	}

	had := provider.SessionReasoningOverride()
	provider.ClearSessionReasoningOverride()
	if had == "" {
		return InfoResult(fmt.Sprintf(
			"No session reasoning override was set; nothing to clear. Provider default (providers.%s.reasoningEffort) remains: %s",
			eff.ProviderID,
			defaultOrServer(eff.ReasoningEffort),
		))
	}
	return InfoResult(fmt.Sprintf(
		"Session reasoning override cleared. Falling back to provider default: %s.",
		defaultOrServer(eff.ReasoningEffort),
	))
}

func handleReasoningSave(ctx *Context) Result {
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Reasoning commands unavailable: settings not loaded.")
	}
	eff, err := provider.EffectiveProvider(ctx.Deps.Settings)
	if err != nil {
		return ErrorResult(err)
	}
	if eff.WireFormat != config.WireFormatOpenAIResponses {
		return reasoningNotApplicable(eff.WireFormat)
	}

	level := strings.TrimSpace(ctx.Args)
	if level == "" {
		return InfoResult("Usage: /reasoning save <level>  (level: minimal | low | medium | high)")
	}
	if !provider.IsValidReasoningLevel(level) {
		return InfoResult(fmt.Sprintf("Unknown reasoning level %q. Expected one of: minimal, low, medium, high.", level))
	}
	if err := provider.SetProviderReasoningEffort(ctx.Deps.Settings, eff.ProviderID, level); err != nil {
		return ErrorResult(err)
	}
	if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
		return ErrorResult(fmt.Errorf("persist reasoning effort: %w", err))
	}
	provider.ClearSessionReasoningOverride()
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
	eff, err := provider.EffectiveProvider(ctx.Deps.Settings)
	if err != nil {
		return ErrorResult(err)
	}
	if eff.WireFormat != config.WireFormatOpenAIResponses {
		return reasoningNotApplicable(eff.WireFormat)
	}
	provider.SetSessionReasoningOverride(level)
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
	case "clear":
		return handleReasoningClear(ctx)
	case "save":
		saveCtx := *ctx
		saveCtx.Args = strings.TrimSpace(strings.TrimPrefix(args, "save"))
		return handleReasoningSave(&saveCtx)
	default:
		return InfoResult("Unknown sub-command '" + head + "'. Expected: show, clear, save <level>, or one of minimal | low | medium | high.")
	}
}

func reasoningNotApplicable(wireFormat config.WireFormat) Result {
	detected := "no active provider"
	if wireFormat != "" {
		detected = fmt.Sprintf("wire format '%s'", wireFormat)
	}
	return InfoResult(fmt.Sprintf(
		"Reasoning effort only applies to OpenAI Responses API providers (wireFormat: openai-responses); active provider has %s. Switch with /provider use <id> first, or run /provider list.",
		detected,
	))
}

func defaultOrServer(value string) string {
	if value == "" {
		return "(server default)"
	}
	return value
}
