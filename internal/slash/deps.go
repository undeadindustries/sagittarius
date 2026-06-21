package slash

import (
	"context"

	"github.com/undeadindustries/sagittarius/internal/agents"
	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/mcp"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/skills"
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
	SkillList() []skills.Definition
	AgentList() []agents.Definition
}

// Deps supplies slash command dependencies (injectable for tests).
type Deps struct {
	Loader   *config.Loader
	Settings *config.Settings
	Hooks    Hooks
}
