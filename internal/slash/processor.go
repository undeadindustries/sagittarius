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
	// DialogSettings opens the curated settings browser.
	DialogSettings DialogKind = "settings"
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
	// Scrollback, when non-empty, asks the interactive TUI to repaint prior
	// conversation turns into the scrollback (used by /chat resume). Entries are
	// emitted before Messages so a restored conversation appears above the
	// command's confirmation line.
	Scrollback []ScrollbackEntry
	// ClearScrollback asks the interactive TUI to clear the existing scrollback
	// before the Scrollback entries are repainted, so /chat resume replaces the
	// visible conversation instead of appending the restored turns beneath it.
	ClearScrollback bool
	// Clipboard, when non-empty, asks the UI layer to copy this text to the
	// system clipboard (used by /copy). The slash layer never touches the
	// terminal itself; the consumer (TUI or headless) performs the copy.
	Clipboard string
	// SubmitPrompt, when non-empty, asks the agent layer to run this text as a
	// follow-up model turn after the command's messages, merging the turn's
	// events into the same stream. Used by /init so the agent analyzes the
	// project and writes AGENTS.md with its tools.
	SubmitPrompt string
	// ThemeName, when non-empty, asks the UI to switch its active theme live
	// ("default" or "greyscale"). Set by /theme.
	ThemeName string
	// MouseMode, when non-empty, asks the UI to enable/disable mouse-wheel
	// reporting live ("on", "off", or "toggle"). Set by /mouse.
	MouseMode string
	// CompleteGrillAfter asks the agent layer to mark the active grill session
	// StatusComplete once the SubmitPrompt turn finishes (set by /grill done,
	// whose SubmitPrompt writes the spec file as the turn's only action).
	CompleteGrillAfter bool
}

// ScrollRole classifies a restored scrollback entry so the TUI can apply the
// matching user / assistant / info styling.
type ScrollRole int

const (
	// ScrollUser is a user turn.
	ScrollUser ScrollRole = iota
	// ScrollAssistant is a model turn.
	ScrollAssistant
	// ScrollInfo is system / informational text.
	ScrollInfo
)

// ScrollbackEntry is one restored conversation block for TUI repaint.
type ScrollbackEntry struct {
	Role ScrollRole
	Text string
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
