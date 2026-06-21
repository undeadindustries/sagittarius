package slash

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func helpCommand() Command {
	return Command{
		Name:        "help",
		Description: "List slash commands and subcommands",
		Handler: func(ctx *Context) Result {
			reg := NewRegistry()
			return InfoResult(reg.RenderHelp())
		},
	}
}

func quitCommand() Command {
	return Command{
		Name:        "quit",
		Description: "Exit the interactive session",
		Handler: func(_ *Context) Result {
			return Result{Handled: true, Quit: true}
		},
	}
}

func providerCommand() Command {
	return Command{
		Name:        "provider",
		Description: "Manage providers (list, switch, configure custom backends)",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List configured providers and active state",
				Handler:     handleProviderList,
			},
			{
				Name:        "use",
				Description: "Switch the active provider",
				Handler:     handleProviderUse,
			},
			{
				Name:        "show",
				Description: "Show the active provider configuration",
				Handler:     handleProviderShow,
			},
			{
				Name:        "set",
				Description: "Set a provider field (model, baseUrl, key for non-Gemini)",
				Handler:     handleProviderSet,
			},
			{
				Name:        "add",
				Description: "Add a custom OpenAI-compatible provider",
				Handler:     handleProviderAdd,
			},
			{
				Name:        "remove",
				Description: "Remove a custom provider",
				Handler:     handleProviderRemove,
			},
		},
		Handler: handleProviderList,
	}
}

func modelCommand() Command {
	return Command{
		Name:        "model",
		Description: "List or set the model for the active provider",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List chat models from the active provider endpoint",
				Handler:     handleModelList,
			},
		},
		Handler: handleModelSet,
	}
}

func authCommand() Command {
	return Command{
		Name:        "auth",
		Description: "Store an API key for the active provider",
		SubCommands: []Command{
			{
				Name:        "set",
				Description: "Store an API key for the active provider",
				Handler:     handleAuthSet,
			},
		},
		Handler: handleAuthSet,
	}
}

func memoryCommand() Command {
	return Command{
		Name:        "memory",
		Description: "Manage project memory (GEMINI.md / AGENTS.md)",
		SubCommands: []Command{
			{
				Name:        "reload",
				Description: "Reload memory files into the system prompt",
				Handler:     handleMemoryReload,
			},
		},
	}
}

func skillsCommand() Command {
	return Command{
		Name:        "skills",
		Description: "Manage agent skills (Phase 12)",
		SubCommands: []Command{
			{
				Name:        "reload",
				Description: "Reload discovered skills (stub until Phase 12)",
				Handler:     handleSkillsReload,
			},
		},
	}
}

func handleProviderList(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}
	s := ctx.Deps.Settings
	active := s.ActiveProvider()

	var lines []string
	lines = append(lines, "Hosted (Gemini):")
	for id, def := range config.BuiltInProviders {
		if def.WireFormat != config.WireFormatGemini {
			continue
		}
		lines = append(lines, formatProviderLine(s, string(id), def.DisplayName, active))
	}

	lines = append(lines, "", "Hosted (OpenAI Chat Completions):")
	for id, def := range config.BuiltInProviders {
		if def.WireFormat != config.WireFormatOpenAIChat {
			continue
		}
		lines = append(lines, formatProviderLine(s, string(id), def.DisplayName, active))
	}

	lines = append(lines, "", "Hosted (OpenAI Responses):")
	for id, def := range config.BuiltInProviders {
		if def.WireFormat != config.WireFormatOpenAIResponses {
			continue
		}
		lines = append(lines, formatProviderLine(s, string(id), def.DisplayName, active))
	}

	if s.Providers != nil && len(s.Providers.Custom) > 0 {
		lines = append(lines, "", "Custom (user-defined):")
		for id, custom := range s.Providers.Custom {
			name := custom.DisplayName
			if name == "" {
				name = id
			}
			lines = append(lines, formatProviderLine(s, id, name+" [custom]", active))
		}
	} else {
		lines = append(lines, "", "No custom providers. Add one with /provider add <id> <baseUrl> [displayName]")
	}

	if active == "" {
		lines = append(lines, "", "No active provider. Run /provider use <id> to select one.")
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func formatProviderLine(s *config.Settings, id, displayName, active string) string {
	marker := "  "
	if id == active {
		marker = "> "
	}
	line := fmt.Sprintf("%s%s (%s)", marker, id, displayName)
	endpoint, err := provider.ResolveEndpointConfig(providerSettingsForID(s, id))
	if err != nil {
		return line + fmt.Sprintf("\n    [config error: %v]", err)
	}
	line += fmt.Sprintf("\n    model: %s", endpoint.Model)
	if endpoint.BaseURL != "" {
		line += fmt.Sprintf("\n    baseUrl: %s", endpoint.BaseURL)
	}
	return line
}

// providerSettingsForID builds a ephemeral settings view with providerID active.
func providerSettingsForID(s *config.Settings, providerID string) *config.Settings {
	clone := *s
	if clone.Providers == nil {
		clone.Providers = &config.ProvidersSettings{}
	}
	prov := *clone.Providers
	prov.Active = providerID
	clone.Providers = &prov
	return &clone
}

func handleProviderUse(ctx *Context) Result {
	id := strings.TrimSpace(ctx.Args)
	if id == "" {
		return InfoResult("Usage: /provider use <id>  (run /provider list to see ids)")
	}
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}
	if err := provider.SaveActiveProvider(ctx.Deps.Loader, ctx.Deps.Settings, id); err != nil {
		return ErrorResult(fmt.Errorf("switch provider: %w", err))
	}
	label, model, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx)
	if err != nil {
		return ErrorResult(fmt.Errorf("rebuild runner after provider switch: %w", err))
	}
	msg := fmt.Sprintf("Active provider → %s. Live on the next request.", id)
	if label != "" {
		msg += fmt.Sprintf(" (%s, model %s)", label, model)
	}
	if def, ok := config.LookupBuiltInProvider(id); ok && def.WireFormat == config.WireFormatGemini {
		msg += " Use /auth to set your API key if needed."
	}
	return InfoResult(msg)
}

func handleProviderShow(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}
	endpoint, err := provider.ResolveEndpointConfig(ctx.Deps.Settings)
	if err != nil {
		return ErrorResult(err)
	}
	lines := []string{
		fmt.Sprintf("Active provider: %s", endpoint.ProviderID),
		fmt.Sprintf("Model: %s", endpoint.Model),
		fmt.Sprintf("Wire format: %s", endpoint.WireFormat),
	}
	if endpoint.BaseURL != "" {
		lines = append(lines, fmt.Sprintf("Base URL: %s", endpoint.BaseURL))
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleProviderSet(ctx *Context) Result {
	parsed, errMsg := parseProviderSetArgs(ctx.Args)
	if errMsg != "" {
		return InfoResult(errMsg)
	}
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}

	def, ok := config.LookupBuiltInProvider(parsed.ID)
	if ok && def.WireFormat == config.WireFormatGemini {
		return InfoResult("Gemini providers use upstream defaults. Use /auth for credentials, or /provider use <id> to switch.")
	}

	if !providerExists(ctx.Deps.Settings, parsed.ID) {
		return InfoResult(fmt.Sprintf("Unknown provider %q. Run /provider list.", parsed.ID))
	}

	switch parsed.Field {
	case "key":
		if parsed.Value == "" {
			return InfoResult(fmt.Sprintf("Usage: /provider set %s key <api-key>", parsed.ID))
		}
		if err := ctx.Deps.Hooks.SetProviderAPIKey(ctx.Ctx, parsed.ID, parsed.Value); err != nil {
			return ErrorResult(fmt.Errorf("save key for %s: %w", parsed.ID, err))
		}
		if ctx.Deps.Settings.ActiveProvider() == parsed.ID {
			if _, _, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx); err != nil {
				return ErrorResult(fmt.Errorf("rebuild runner after key update: %w", err))
			}
		}
		return InfoResult(fmt.Sprintf("API key saved for %s (%s).", parsed.ID, credentials.Redact(parsed.Value)))
	case "model", "baseurl", "baseUrl":
		field := parsed.Field
		if field == "baseurl" {
			field = "baseUrl"
		}
		if err := provider.SetProviderField(ctx.Deps.Settings, parsed.ID, field, parsed.Value); err != nil {
			return ErrorResult(err)
		}
		if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
			return ErrorResult(fmt.Errorf("persist provider settings: %w", err))
		}
		if ctx.Deps.Settings.ActiveProvider() == parsed.ID {
			if _, _, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx); err != nil {
				return ErrorResult(fmt.Errorf("rebuild runner after field update: %w", err))
			}
		}
		return InfoResult(fmt.Sprintf("Set %s %s → %s", parsed.ID, field, parsed.Value))
	default:
		return InfoResult("Supported fields: model, baseUrl, key")
	}
}

type providerSetArgs struct {
	ID    string
	Field string
	Value string
}

func parseProviderSetArgs(args string) (providerSetArgs, string) {
	parts := strings.Fields(strings.TrimSpace(args))
	if len(parts) < 2 {
		return providerSetArgs{}, "Usage: /provider set <id> <field> <value>  (field: model | baseUrl | key)"
	}
	id, field := parts[0], parts[1]
	value := strings.TrimSpace(strings.TrimPrefix(args, id))
	value = strings.TrimSpace(strings.TrimPrefix(value, field))
	return providerSetArgs{ID: id, Field: field, Value: value}, ""
}

func handleProviderAdd(ctx *Context) Result {
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	if len(parts) < 2 {
		return InfoResult("Usage: /provider add <id> <baseUrl> [displayName] [apiKeyEnvVar]")
	}
	id, baseURL := parts[0], parts[1]
	displayName := id
	if len(parts) > 2 {
		displayName = parts[2]
	}
	envVar := ""
	if len(parts) > 3 {
		envVar = parts[3]
	}
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}
	if _, ok := config.LookupBuiltInProvider(id); ok {
		return InfoResult(fmt.Sprintf("Cannot add built-in provider %q. Choose a new custom id.", id))
	}
	if err := provider.AddCustomProvider(ctx.Deps.Settings, id, config.CustomProviderDefinition{
		DisplayName:  displayName,
		BaseURL:      baseURL,
		APIKeyEnvVar: envVar,
	}); err != nil {
		return ErrorResult(err)
	}
	if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
		return ErrorResult(fmt.Errorf("persist custom provider: %w", err))
	}
	return InfoResult(fmt.Sprintf("Added custom provider %q (%s). Run /provider use %s to activate.", id, displayName, id))
}

func handleProviderRemove(ctx *Context) Result {
	id := strings.TrimSpace(ctx.Args)
	if id == "" {
		return InfoResult("Usage: /provider remove <id>")
	}
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Provider commands unavailable: settings not loaded.")
	}
	if _, ok := config.LookupBuiltInProvider(id); ok {
		return InfoResult(fmt.Sprintf("Cannot remove built-in provider %q.", id))
	}
	if err := provider.RemoveCustomProvider(ctx.Deps.Settings, id); err != nil {
		return ErrorResult(err)
	}
	if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
		return ErrorResult(fmt.Errorf("persist provider removal: %w", err))
	}
	return InfoResult(fmt.Sprintf("Removed custom provider %q.", id))
}

func handleModelList(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Model commands unavailable: settings not loaded.")
	}
	models := ctx.Deps.Hooks.DiscoverModels(ctx.Ctx)
	if len(models) == 0 {
		return InfoResult("No models discovered from the active provider endpoint.")
	}
	lines := make([]string, 0, len(models)+1)
	lines = append(lines, "Chat models:")
	for _, m := range models {
		lines = append(lines, "  "+m.ID)
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleModelSet(ctx *Context) Result {
	model := strings.TrimSpace(ctx.Args)
	if model == "" {
		return InfoResult("Usage: /model <name>  or  /model list")
	}
	if strings.EqualFold(model, "list") {
		return handleModelList(ctx)
	}
	if ctx.Deps.Loader == nil || ctx.Deps.Settings == nil {
		return InfoResult("Model commands unavailable: settings not loaded.")
	}
	active := ctx.Deps.Settings.ActiveProvider()
	if active == "" {
		return InfoResult("No active provider. Run /provider use <id> first.")
	}
	if err := provider.SetProviderModel(ctx.Deps.Settings, active, model); err != nil {
		return ErrorResult(err)
	}
	if err := ctx.Deps.Loader.Save(ctx.Deps.Settings); err != nil {
		return ErrorResult(fmt.Errorf("persist model: %w", err))
	}
	if _, _, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx); err != nil {
		return ErrorResult(fmt.Errorf("rebuild runner after model change: %w", err))
	}
	return InfoResult(fmt.Sprintf("Model set to %s for provider %s.", model, active))
}

func handleAuthSet(ctx *Context) Result {
	if ctx.Deps.Settings == nil {
		return InfoResult("Auth commands unavailable: settings not loaded.")
	}
	key := strings.TrimSpace(ctx.Args)
	if strings.HasPrefix(strings.ToLower(key), "set ") {
		key = strings.TrimSpace(key[4:])
	}
	if key == "" {
		return InfoResult("Usage: /auth <api-key>  or  /auth set <api-key>")
	}
	active := ctx.Deps.Settings.ActiveProvider()
	if active == "" {
		return InfoResult("No active provider. Run /provider use <id> first.")
	}
	if err := ctx.Deps.Hooks.SetProviderAPIKey(ctx.Ctx, active, key); err != nil {
		return ErrorResult(fmt.Errorf("save api key: %w", err))
	}
	if _, _, err := ctx.Deps.Hooks.RebuildRunner(ctx.Ctx); err != nil {
		return ErrorResult(fmt.Errorf("rebuild runner after auth: %w", err))
	}
	return InfoResult(fmt.Sprintf("API key stored for %s (%s).", active, credentials.Redact(key)))
}

func handleMemoryReload(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ReloadSystemInstruction(ctx.Ctx); err != nil {
		return ErrorResult(fmt.Errorf("reload memory: %w", err))
	}
	return InfoResult("Memory reloaded from GEMINI.md / AGENTS.md.")
}

func handleSkillsReload(_ *Context) Result {
	return InfoResult("Skills reload is a stub until Phase 12 (MCP, skills, extensions).")
}

func providerExists(s *config.Settings, id string) bool {
	if _, ok := config.LookupBuiltInProvider(id); ok {
		return true
	}
	if s.Providers != nil {
		if _, ok := s.Providers.Custom[id]; ok {
			return true
		}
	}
	return false
}
