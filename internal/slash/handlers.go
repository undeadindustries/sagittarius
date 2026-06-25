package slash

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/version"
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

func settingsCommand() Command {
	return Command{
		Name:        "settings",
		Description: "Browse and edit global and project settings",
		Handler:     func(_ *Context) Result { return DialogResult(DialogSettings) },
	}
}

func aboutCommand() Command {
	return Command{
		Name:        "about",
		Description: "Show version info. Share this information when filing issues.",
		Handler:     handleAbout,
	}
}

// handleAbout renders the CLI version, Go toolchain, and platform so users can
// paste it into bug reports.
func handleAbout(_ *Context) Result {
	var sb strings.Builder
	sb.WriteString("Sagittarius CLI\n")
	sb.WriteString("Version: " + version.String() + "\n")
	sb.WriteString("Go: " + runtime.Version() + "\n")
	sb.WriteString("OS/Arch: " + runtime.GOOS + "/" + runtime.GOARCH)
	return InfoResult(sb.String())
}

func compressCommand() Command {
	return Command{
		Name:        "compress",
		Description: "Replace the chat context with a summary to save tokens",
		Handler:     handleCompress,
	}
}

// handleCompress manually compresses the conversation context into a summary.
func handleCompress(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Compression unavailable.")
	}
	msg, err := ctx.Deps.Hooks.ForceCompressHistory(ctx.Ctx)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(msg)
}

func copyCommand() Command {
	return Command{
		Name:        "copy",
		Description: "Copy the last assistant response to the clipboard",
		Handler:     handleCopy,
	}
}

// handleCopy copies the most recent assistant response to the clipboard. The
// actual copy is performed by the UI layer via Result.Clipboard so the slash
// layer stays free of terminal I/O.
func handleCopy(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Clipboard unavailable.")
	}
	text := ctx.Deps.Hooks.LastAssistantText()
	if text == "" {
		return InfoResult("No assistant response to copy yet.")
	}
	return Result{Handled: true, Clipboard: text}
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
		Name:        "providers",
		Description: "Manage providers — edit connections, API keys, and activate models per provider",
		Handler:     handleProviders,
	}
}

// handleProviders opens the interactive providers wizard (menu-first, no
// subcommands). The TUI consumes the dialog result and drives every provider
// operation; headless mode never processes slash commands.
func handleProviders(_ *Context) Result {
	return DialogResult(DialogProviders)
}

func modelCommand() Command {
	return Command{
		Name:        "model",
		Description: "Pick the current {Provider}/{Model} from the global active list — opens an interactive picker or accepts a direct argument",
		Handler:     handleModelPick,
		ArgComplete: func(deps Deps, argPrefix string) []string {
			pairs := deps.Hooks.AllActiveModels()
			var out []string
			for _, p := range pairs {
				label := p.DisplayID + "/" + p.Model
				if strings.HasPrefix(label, argPrefix) {
					out = append(out, label)
				}
			}
			return out
		},
	}
}

// handleModelPick selects a model directly when an argument is provided, or
// opens the picker dialog for interactive selection.
func handleModelPick(ctx *Context) Result {
	arg := strings.TrimSpace(ctx.Args)
	if arg == "" {
		return DialogResult(DialogModelPick)
	}
	// Parse "{DisplayID}/{model}" or "{canonicalID}/{model}".
	parts := strings.SplitN(arg, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return ErrorResult(fmt.Errorf("/model: expected {provider}/{model}, got %q", arg))
	}
	displayOrID := parts[0]
	model := parts[1]
	// Resolve display id to canonical id by scanning the active-model list.
	pairs := ctx.Deps.Hooks.AllActiveModels()
	providerID := ""
	for _, p := range pairs {
		if (p.DisplayID == displayOrID || p.ProviderID == displayOrID) && p.Model == model {
			providerID = p.ProviderID
			break
		}
	}
	if providerID == "" {
		return ErrorResult(fmt.Errorf("/model: %q is not in the active model list; run /model to see available models", arg))
	}
	resolved, err := ctx.Deps.Hooks.SelectCurrentModel(ctx.Ctx, providerID, model)
	if err != nil {
		return ErrorResult(err)
	}
	msg := fmt.Sprintf("Model → %s/%s", displayOrID, model)
	if resolved != "" && resolved != model {
		msg += fmt.Sprintf(" (mode override active: using %s)", resolved)
	}
	return InfoResult(msg)
}

func modelsCommand() Command {
	return Command{
		Name:        "models",
		Description: "Edit per-model settings (temperature, context limit, reasoning effort) — opens an interactive menu",
		Handler:     handleModels,
	}
}

func systemPromptCommand() Command {
	return Command{
		Name:        "system-prompt",
		Description: "Set the project system-prompt personality (saved to .sagittarius/settings.json) — opens a picker or accepts a preset id",
		Handler:     handleSystemPrompt,
		ArgComplete: func(_ Deps, argPrefix string) []string {
			var out []string
			for _, p := range config.SortedSystemPromptPresets() {
				if strings.HasPrefix(p.ID, argPrefix) {
					out = append(out, p.ID)
				}
			}
			return out
		},
	}
}

// handleSystemPrompt opens the picker when no argument is given, or applies a
// preset id directly for headless use.
func handleSystemPrompt(ctx *Context) Result {
	arg := strings.TrimSpace(ctx.Args)
	if arg == "" {
		return DialogResult(DialogSystemPrompt)
	}
	msg, err := ctx.Deps.Hooks.ApplyProjectSystemPromptPreset(ctx.Ctx, arg)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(msg)
}

// handleModels opens the per-model settings editor dialog.
func handleModels(_ *Context) Result {
	return DialogResult(DialogModels)
}

func modesCommand() Command {
	return Command{
		Name:        "modes",
		Description: "Edit mode overrides (assign a {Provider}/{Model} to a mode or clear to default) — opens an interactive menu",
		SubCommands: []Command{
			{
				Name:        "override",
				Description: "Set a mode routing override headlessly (scope: global|project; default project). Usage: /modes override <agent|plan|ask|debug> <Provider/Model> [global|project]",
				Handler:     handleModesOverride,
			},
			{
				Name:        "clear",
				Description: "Clear a mode routing override headlessly. Usage: /modes clear <agent|plan|ask|debug> [global|project]",
				Handler:     handleModesClear,
			},
		},
		Handler: handleModes,
	}
}

// handleModes opens the mode-override editor dialog.
func handleModes(_ *Context) Result {
	return DialogResult(DialogModes)
}

// handleModesOverride sets a mode override headlessly:
//
//	/modes override <agent|plan|ask|debug> <Provider/Model> [global|project]
func handleModesOverride(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Mode override unavailable.")
	}
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	if len(parts) < 2 {
		return InfoResult("Usage: /modes override <agent|plan|ask|debug> <Provider/Model> [global|project]")
	}
	modeName := strings.ToLower(parts[0])
	pair := parts[1]
	scope := config.ScopeProject
	if len(parts) >= 3 {
		var err error
		scope, err = parseScope(parts[2])
		if err != nil {
			return InfoResult(err.Error())
		}
	}
	slash := strings.SplitN(pair, "/", 2)
	if len(slash) != 2 || slash[0] == "" || slash[1] == "" {
		return InfoResult("Model must be in Provider/Model format, e.g. openrouter/qwen/qwen3-235b-a22b")
	}
	if err := ctx.Deps.Hooks.SetModeOverride(ctx.Ctx, modeName, slash[0], slash[1], scope); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Mode %s override → %s (saved to %s settings)", modeName, pair, scope))
}

// handleModesClear clears a mode override headlessly:
//
//	/modes clear <agent|plan|ask|debug> [global|project]
func handleModesClear(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Mode override unavailable.")
	}
	parts := strings.Fields(strings.TrimSpace(ctx.Args))
	if len(parts) < 1 {
		return InfoResult("Usage: /modes clear <agent|plan|ask|debug> [global|project]")
	}
	modeName := strings.ToLower(parts[0])
	scope := config.ScopeProject
	if len(parts) >= 2 {
		var err error
		scope, err = parseScope(parts[1])
		if err != nil {
			return InfoResult(err.Error())
		}
	}
	if err := ctx.Deps.Hooks.SetModeOverride(ctx.Ctx, modeName, "", "", scope); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Mode %s override cleared from %s settings", modeName, scope))
}

func parseScope(s string) (config.SettingScope, error) {
	switch strings.ToLower(s) {
	case "global", "user":
		return config.ScopeGlobal, nil
	case "project", "workspace":
		return config.ScopeProject, nil
	default:
		return config.ScopeProject, fmt.Errorf("unknown scope %q (expected global or project)", s)
	}
}

func memoryCommand() Command {
	return Command{
		Name:        "memory",
		Description: "Manage project memory (AGENTS.md)",
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
		Description: "Manage agent skills (list, reload)",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List discovered skills",
				Handler:     handleSkillsList,
			},
			{
				Name:        "reload",
				Description: "Reload discovered skills from disk and extensions",
				Handler:     handleSkillsReload,
			},
		},
		Handler: handleSkillsList,
	}
}

func mcpCommand() Command {
	return Command{
		Name:        "mcp",
		Description: "Manage MCP servers (add, edit, remove, reload) — opens an interactive wizard",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List configured MCP servers and status",
				Handler:     handleMCPList,
			},
			{
				Name:        "reload",
				Description: "Reload MCP servers and rediscover tools",
				Handler:     handleMCPReload,
			},
		},
		Handler: handleMCP,
	}
}

// handleMCP opens the MCP server wizard, or lists servers as text in headless mode.
func handleMCP(_ *Context) Result {
	return DialogResult(DialogMCP)
}

func toolsCommand() Command {
	return Command{
		Name:        "tools",
		Description: "Browse the effective tool inventory (built-ins + MCP) — opens an interactive view",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List built-in and MCP tools as text",
				Handler:     handleToolsList,
			},
			{
				Name:        "desc",
				Description: "List tools with descriptions",
				Handler:     handleToolsDesc,
			},
		},
		Handler: handleTools,
	}
}

// handleTools opens the tool inventory overlay.
func handleTools(_ *Context) Result {
	return DialogResult(DialogTools)
}

func handleToolsList(ctx *Context) Result {
	return toolsTextResult(ctx, false)
}

func handleToolsDesc(ctx *Context) Result {
	return toolsTextResult(ctx, true)
}

func toolsTextResult(ctx *Context, withDesc bool) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Tools unavailable.")
	}
	lines := []string{"Built-in tools (not editable):"}
	for _, tool := range ctx.Deps.Hooks.BuiltinTools() {
		lines = append(lines, "  "+formatToolLine(tool.Name, tool.Description, withDesc))
	}

	inventory := ctx.Deps.Hooks.MCPToolInventory(ctx.Ctx)
	if len(inventory) == 0 {
		lines = append(lines, "", "No MCP servers configured.")
		return InfoResult(strings.Join(lines, "\n"))
	}
	for _, group := range inventory {
		header := fmt.Sprintf("%s (%s)", group.Server, group.Status)
		lines = append(lines, "", header+":")
		if group.Err != "" {
			lines = append(lines, "  error: "+group.Err)
			continue
		}
		if len(group.Tools) == 0 {
			lines = append(lines, "  (no tools)")
			continue
		}
		for _, tool := range group.Tools {
			state := "on"
			if !tool.Enabled {
				state = "off"
			}
			label := fmt.Sprintf("%s [%s]", tool.Name, state)
			lines = append(lines, "  "+formatToolLine(label, tool.Description, withDesc))
		}
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func formatToolLine(name, description string, withDesc bool) string {
	if withDesc && strings.TrimSpace(description) != "" {
		return fmt.Sprintf("%s — %s", name, description)
	}
	return name
}

func agentsCommand() Command {
	return Command{
		Name:        "agents",
		Description: "Manage local agents (list, reload)",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List discovered agent definitions",
				Handler:     handleAgentsList,
			},
			{
				Name:        "reload",
				Description: "Reload agent definitions from disk and extensions",
				Handler:     handleAgentsReload,
			},
		},
		Handler: handleAgentsList,
	}
}

func resumeCommand() Command {
	return Command{
		Name:        "resume",
		Description: "List sessions or show info about resuming (use --resume flag to resume on startup)",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List available sessions for the current project",
				Handler:     handleResumeList,
			},
		},
		Handler: handleResumeList,
	}
}

func clearCommand() Command {
	return Command{
		Name:        "clear",
		Description: "Clear the current conversation history (start a fresh turn)",
		Handler:     handleClear,
	}
}

func handleResumeList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Session commands unavailable.")
	}
	infos, err := ctx.Deps.Hooks.ListSessions()
	if err != nil {
		return ErrorResult(fmt.Errorf("list sessions: %w", err))
	}
	return InfoResult(session.FormatSessionList(infos))
}

func handleClear(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Clear unavailable.")
	}
	if err := ctx.Deps.Hooks.ClearHistory(); err != nil {
		return ErrorResult(fmt.Errorf("clear history: %w", err))
	}
	return InfoResult("Conversation history cleared. Starting fresh.")
}

func handleMemoryReload(ctx *Context) Result {
	if err := ctx.Deps.Hooks.ReloadSystemInstruction(ctx.Ctx); err != nil {
		return ErrorResult(fmt.Errorf("reload memory: %w", err))
	}
	return InfoResult("Memory reloaded from AGENTS.md.")
}

func handleSkillsReload(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Skills reload unavailable.")
	}
	msg, err := ctx.Deps.Hooks.ReloadSkills(ctx.Ctx)
	if err != nil {
		return ErrorResult(fmt.Errorf("reload skills: %w", err))
	}
	return InfoResult(msg)
}

func handleSkillsList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Skills unavailable.")
	}
	skillsList := ctx.Deps.Hooks.SkillList()
	if len(skillsList) == 0 {
		return InfoResult("No skills discovered.")
	}
	lines := []string{"Discovered skills:"}
	for _, skill := range skillsList {
		lines = append(lines, fmt.Sprintf("  %s — %s", skill.Name, skill.Description))
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleMCPReload(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("MCP reload unavailable.")
	}
	msg, err := ctx.Deps.Hooks.ReloadMCP(ctx.Ctx)
	if err != nil {
		return ErrorResult(fmt.Errorf("reload mcp: %w", err))
	}
	return InfoResult(msg)
}

func handleMCPList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("MCP unavailable.")
	}
	states := ctx.Deps.Hooks.MCPStates()
	if len(states) == 0 {
		return InfoResult("No MCP servers configured.")
	}
	lines := []string{"MCP servers:"}
	for _, st := range states {
		line := fmt.Sprintf("  %s: %s (%d tools)", st.Name, st.Status, st.ToolCount)
		if st.LastError != "" {
			line += " — " + st.LastError
		}
		lines = append(lines, line)
	}
	return InfoResult(strings.Join(lines, "\n"))
}

func handleAgentsReload(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Agents reload unavailable.")
	}
	summary, err := ctx.Deps.Hooks.ReloadAgents(ctx.Ctx)
	if err != nil {
		return ErrorResult(fmt.Errorf("reload agents: %w", err))
	}
	return InfoResult(agents.FormatSummary(summary))
}

func handleAgentsList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Agents unavailable.")
	}
	defs := ctx.Deps.Hooks.AgentList()
	if len(defs) == 0 {
		return InfoResult("No agents discovered.")
	}
	lines := []string{"Discovered agents:"}
	for _, def := range defs {
		lines = append(lines, fmt.Sprintf("  %s — %s (%s)", def.Name, def.Description, def.Kind))
	}
	return InfoResult(strings.Join(lines, "\n"))
}
