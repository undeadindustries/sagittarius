package slash

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/bgproc"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/skills"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

// Hooks performs runner and credential side effects for slash commands.
type Hooks interface {
	RebuildRunner(ctx context.Context) (providerLabel, model string, err error)
	ReloadSystemInstruction(ctx context.Context) error
	DiscoverModels(ctx context.Context) []provider.ModelInfo
	SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error
	ReloadMCP(ctx context.Context) (string, error)
	ReloadSkills(ctx context.Context) (string, error)
	ReloadAgents(ctx context.Context) (agents.ReloadSummary, error)
	MCPStates() []mcp.ServerState
	// MCPToolInventory returns the unfiltered per-server tool list with enabled
	// flags, for the headless `/tools` listing.
	MCPToolInventory(ctx context.Context) []mcp.ServerToolInventory
	// BuiltinTools returns code-defined tools (built-in + skill) for `/tools`.
	BuiltinTools() []tools.ToolEntry
	SkillList() []skills.Definition
	AgentList() []agents.Definition
	// Session hooks — may be nil when no session manager is active.
	ListSessions() ([]session.SessionInfo, error)
	ClearHistory() error
	// Interaction mode hooks (Phase 15).
	SetInteractionMode(ctx context.Context, mode modes.Mode) (model string, err error)
	InteractionMode() (mode modes.Mode, model string)
	// Snapshot hooks (local diffs + undo). SnapshotDiff returns the net unified
	// diff of this session's file changes (empty when none); SnapshotUndo
	// reverts the last n changes and returns the restored relative paths.
	SnapshotDiff(pathFilter string) (string, error)
	SnapshotUndo(n int) ([]string, error)
	// SelectCurrentModel atomically switches to the given (provider, model) pair
	// and rebuilds the runner. Returns the resolved live model or an error.
	SelectCurrentModel(ctx context.Context, providerID, model string) (string, error)
	// AllActiveModels returns every curated (provider, model) pair across all
	// providers, for the /model picker and autocomplete.
	AllActiveModels() []provider.ProviderModelPair
	// ProjectSystemPromptPresetID returns the active project system-prompt preset id.
	ProjectSystemPromptPresetID() string
	// ApplyProjectSystemPromptPreset writes a preset to the project settings file
	// and reloads the runner system instruction.
	ApplyProjectSystemPromptPreset(ctx context.Context, presetID string) (string, error)
	// Chat checkpoint + export hooks (/chat).
	// WriteRequestDebug writes the most recent provider request to a timestamped
	// JSON file in the working directory and returns its path.
	WriteRequestDebug() (string, error)
	CurrentHistory() ([]provider.Message, error)
	WorkDir() string
	// SaveCheckpoint persists the current conversation under tag. It refuses to
	// clobber an existing checkpoint unless overwrite is true.
	SaveCheckpoint(tag string, overwrite bool) (string, error)
	ListCheckpoints() ([]string, error)
	// ResumeCheckpoint restores tag into the live session and returns a summary
	// plus the restored conversation for scrollback repaint.
	ResumeCheckpoint(ctx context.Context, tag string) (summary string, history []provider.Message, err error)
	DeleteCheckpoint(tag string) error
	// ForceCompressHistory manually compresses the conversation context into a
	// summary and returns a human-readable result message.
	ForceCompressHistory(ctx context.Context) (string, error)
	// LastAssistantText returns the most recent assistant response text, or ""
	// when there is none. Used by /copy.
	LastAssistantText() string
	// SessionStatsText returns session telemetry formatted as plain text for the
	// /stats command. section is "" or "session" (full summary), "model", or "tools".
	SessionStatsText(section string) string
	// SetUITheme persists the chosen TUI theme ("default" or "greyscale") to
	// settings. Used by /theme; the live switch is driven separately via the UI.
	SetUITheme(name string) error

	// Background process viewer hooks
	ListBackgroundProcesses() []bgproc.Process
	KillBackgroundProcess(pid int) error
	BackgroundProcessOutput(pid int) string
}

// Deps supplies slash command dependencies (injectable for tests).
type Deps struct {
	Loader   *config.Loader
	Settings *config.Settings
	Hooks    Hooks
}
