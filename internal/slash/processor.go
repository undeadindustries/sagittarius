package slash

import (
	"context"
	"fmt"
	"strings"
)

// DialogKind identifies an interactive TUI dialog a command requests to open.
// Empty means no dialog. Headless mode never processes slash commands, so this
// is only meaningful in interactive sessions.
type DialogKind string

const (
	// DialogProviders opens the providers management wizard.
	DialogProviders DialogKind = "providers"
	// DialogModels opens the per-model settings editor.
	DialogModels DialogKind = "models"
	// DialogModelPick opens the global {Provider}/{Model} picker for /model.
	DialogModelPick DialogKind = "model-pick"
	// DialogModes opens the mode-override editor.
	DialogModes DialogKind = "modes"
	// DialogSystemPrompt opens the project system-prompt preset picker.
	DialogSystemPrompt DialogKind = "system-prompt"
	// DialogMCP opens the MCP server management wizard.
	DialogMCP DialogKind = "mcp"
	// DialogTools opens the tool inventory.
	DialogTools DialogKind = "tools"
)

// Result is the outcome of processing one slash command.
type Result struct {
	Handled bool
	Quit    bool
	// OpenDialog, when non-empty, asks the interactive TUI to open a dialog
	// overlay. It is only meaningful in interactive sessions.
	OpenDialog DialogKind
	Messages   []string
	Err        error
}

// Context carries per-invocation state for command handlers.
type Context struct {
	Ctx  context.Context
	Deps Deps
	Args string
	Path []string
}

// Processor dispatches slash commands.
type Processor struct {
	registry *Registry
}

// NewProcessor constructs a Processor with the default built-in registry.
func NewProcessor() *Processor {
	return &Processor{registry: NewRegistry()}
}

// Registry returns the command registry (for tests and /help).
func (p *Processor) Registry() *Registry {
	return p.registry
}

// Process handles a slash command line. Non-slash input returns Handled=false.
func (p *Processor) Process(ctx context.Context, input string, deps Deps) Result {
	if !IsSlashInput(input) {
		return Result{Handled: false}
	}

	parsed := ParseSlashCommand(input, p.registry)
	if parsed.Command == nil {
		return Result{
			Handled:  true,
			Messages: []string{fmt.Sprintf("Unknown command: %s\nRun /help for available commands.", input)},
		}
	}

	if parsed.Command.Handler == nil {
		return Result{
			Handled:  true,
			Messages: []string{usageHint(parsed)},
		}
	}

	cmdCtx := &Context{
		Ctx:  ctx,
		Deps: deps,
		Args: parsed.Args,
		Path: parsed.Path,
	}
	return parsed.Command.Handler(cmdCtx)
}

func usageHint(parsed Parsed) string {
	cmd := parsed.Command
	if len(cmd.SubCommands) == 0 {
		return fmt.Sprintf("Usage: /%s", joinPath(parsed.Path))
	}
	names := make([]string, 0, len(cmd.SubCommands))
	for _, sub := range visibleSubcommands(*cmd) {
		names = append(names, sub.Name)
	}
	return fmt.Sprintf("Usage: /%s <%s>", joinPath(parsed.Path), joinOr(names))
}

func joinPath(path []string) string {
	return strings.Join(path, " ")
}

func joinOr(items []string) string {
	if len(items) == 0 {
		return "subcommand"
	}
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], "|") + "|" + items[len(items)-1]
}

// InfoResult returns a handled result with info messages.
func InfoResult(msgs ...string) Result {
	return Result{Handled: true, Messages: msgs}
}

// DialogResult returns a handled result that asks the TUI to open a dialog.
func DialogResult(kind DialogKind) Result {
	return Result{Handled: true, OpenDialog: kind}
}

// ErrorResult returns a handled result with an error.
func ErrorResult(err error) Result {
	return Result{Handled: true, Err: err}
}
