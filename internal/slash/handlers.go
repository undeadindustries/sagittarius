package slash

import (
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/session"
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
		Name:        "providers",
		Description: "Manage providers — opens an interactive menu (switch, add, edit, remove, activate models)",
		Handler:     handleProviders,
	}
}

// handleProviders opens the interactive providers wizard (menu-first, no
// subcommands). The TUI consumes the dialog result and drives every provider
// operation; headless mode never processes slash commands.
func handleProviders(_ *Context) Result {
	return DialogResult(DialogProviders)
}

func modelsCommand() Command {
	return Command{
		Name:        "models",
		Description: "Pick the active model from the active provider's active models — opens an interactive menu",
		Handler:     handleModels,
	}
}

// handleModels opens the interactive models menu (menu-first, no subcommands),
// scoped to the active provider's curated active models.
func handleModels(_ *Context) Result {
	return DialogResult(DialogModels)
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
		Description: "Manage MCP servers (list, reload)",
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
		Handler: handleMCPList,
	}
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
	return InfoResult("Memory reloaded from GEMINI.md / AGENTS.md.")
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
