package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// AppConfig wires the interactive agent loop with slash command support.
type AppConfig struct {
	Runner        *Runner
	ProviderLabel string
	Model         string
	Loader        *config.Loader
	Settings      *config.Settings
	// SessionID keys the context manager's adaptive state and offload dirs. It
	// is reused across provider switches so offload paths stay stable.
	SessionID string
}

// App adapts Runner to ui.App for interactive TUI sessions.
type App struct {
	runner    *Runner
	status    ui.StatusBar
	processor *slash.Processor
	deps      slash.Deps
	sessionID string
}

// NewApp wraps runner for interactive use and exposes footer metadata.
func NewApp(cfg AppConfig) *App {
	if cfg.ProviderLabel == "" {
		cfg.ProviderLabel = "ready"
	}
	if cfg.Model == "" && cfg.Runner != nil {
		cfg.Model = cfg.Runner.Model()
	}
	app := &App{
		runner:    cfg.Runner,
		processor: slash.NewProcessor(),
		sessionID: cfg.SessionID,
		status: ui.StatusBar{
			Left:  cfg.ProviderLabel,
			Right: cfg.Model,
		},
		deps: slash.Deps{
			Loader:   cfg.Loader,
			Settings: cfg.Settings,
			Hooks:    &appHooks{app: nil},
		},
	}
	app.deps.Hooks = &appHooks{app: app}
	return app
}

// HandleInput implements ui.App. Slash commands are handled locally; other
// input is delegated to the agent runner.
func (a *App) HandleInput(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	if slash.IsSlashInput(input) {
		return a.handleSlash(ctx, input)
	}
	return a.runner.RunTurn(ctx, input)
}

// Status returns footer metadata for the TUI status bar.
func (a *App) Status() ui.StatusBar {
	return a.status
}

func (a *App) handleSlash(ctx context.Context, input string) (<-chan ui.StreamEvent, error) {
	result := a.processor.Process(ctx, input, a.deps)
	out := make(chan ui.StreamEvent, 4)

	go func() {
		defer close(out)
		if result.Quit {
			out <- ui.StreamEvent{Type: ui.StreamQuit}
			out <- ui.StreamEvent{Type: ui.StreamDone}
			return
		}
		for _, msg := range result.Messages {
			out <- ui.StreamEvent{Type: ui.StreamInfo, Text: msg + "\n"}
		}
		if result.Err != nil {
			out <- ui.StreamEvent{Type: ui.StreamError, Err: result.Err}
		}
		out <- ui.StreamEvent{Type: ui.StreamDone}
	}()

	return out, nil
}

type appHooks struct {
	app *App
}

func (h *appHooks) RebuildRunner(ctx context.Context) (string, string, error) {
	if h.app == nil || h.app.runner == nil {
		return "", "", fmt.Errorf("runner not available")
	}
	if h.app.deps.Settings == nil {
		return "", "", fmt.Errorf("settings not loaded")
	}

	gen, err := provider.NewContentGenerator(ctx, h.app.deps.Settings)
	if err != nil {
		return "", "", err
	}
	endpoint, err := provider.ResolveEndpointConfig(h.app.deps.Settings)
	if err != nil {
		return "", "", err
	}

	h.app.runner.SetGenerator(gen)
	h.app.runner.SetModel(endpoint.Model)

	// Rebuild the context manager so local-context defenses track the new wire
	// format. NewContextManager returns nil off the openai-chat path, making
	// context management a pure pass-through for gemini-native / openai-responses.
	h.app.runner.SetContextManager(
		NewContextManager(h.app.deps.Settings, gen, endpoint.Model, h.app.sessionID),
	)

	label := endpoint.ProviderID
	if def, ok := config.LookupBuiltInProvider(endpoint.ProviderID); ok {
		label = def.DisplayName
	} else if h.app.deps.Settings.Providers != nil {
		if custom, ok := h.app.deps.Settings.Providers.Custom[endpoint.ProviderID]; ok && custom.DisplayName != "" {
			label = custom.DisplayName
		}
	}

	h.app.status = ui.StatusBar{
		Left:  label,
		Right: endpoint.Model,
	}
	return label, endpoint.Model, nil
}

func (h *appHooks) ReloadSystemInstruction(ctx context.Context) error {
	if h.app == nil || h.app.runner == nil {
		return fmt.Errorf("runner not available")
	}
	_ = ctx
	return h.app.runner.ReloadSystemInstruction()
}

func (h *appHooks) DiscoverModels(ctx context.Context) []provider.ModelInfo {
	if h.app == nil || h.app.deps.Settings == nil {
		return nil
	}
	endpoint, err := provider.ResolveEndpointConfig(h.app.deps.Settings)
	if err != nil || endpoint.BaseURL == "" {
		return nil
	}
	bearer := endpoint.Bearer
	if bearer == "" && endpoint.RequiresAPIKey {
		key, err := credentials.ResolveProviderAPIKey(ctx, endpoint.ProviderID)
		if err == nil {
			bearer = key
		}
	}
	return provider.DiscoverModels(ctx, endpoint.BaseURL, bearer, nil)
}

func (h *appHooks) SetProviderAPIKey(ctx context.Context, providerID, apiKey string) error {
	return credentials.SetProviderAPIKey(ctx, providerID, apiKey)
}

// SetGenerator replaces the content generator (used after provider changes)
// and clears any recorded provider-unavailable error.
func (r *Runner) SetGenerator(gen provider.ContentGenerator) {
	r.genMu.Lock()
	r.gen = gen
	r.genErr = nil
	r.genMu.Unlock()
}

// SetModel updates the model used for generate requests.
func (r *Runner) SetModel(model string) {
	model = strings.TrimSpace(model)
	if model != "" {
		r.model = model
	}
}

// ReloadSystemInstruction re-reads GEMINI.md / AGENTS.md into the system prompt.
func (r *Runner) ReloadSystemInstruction() error {
	system, err := DiscoverSystemInstruction(r.workDir)
	if err != nil {
		return err
	}
	r.system = system
	return nil
}
