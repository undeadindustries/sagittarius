package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
)

// mapDialogKind translates a slash dialog request into the UI event kind.
func mapDialogKind(kind slash.DialogKind) ui.DialogKind {
	switch kind {
	case slash.DialogProviders:
		return ui.DialogProviders
	case slash.DialogModels:
		return ui.DialogModels
	default:
		return ""
	}
}

// ProviderDialogDeps returns the side-effect adapter the providers wizard uses.
// It is consumed by the Bubble Tea layer when opening the providers overlay.
func (a *App) ProviderDialogDeps() providersdialog.Deps {
	return &providerDialogDeps{app: a}
}

// providerDialogDeps implements providersdialog.Deps over the App's runner,
// loader, settings, and credential store.
type providerDialogDeps struct {
	app *App
}

func (d *providerDialogDeps) settings() *config.Settings { return d.app.deps.Settings }
func (d *providerDialogDeps) loader() *config.Loader     { return d.app.deps.Loader }

func (d *providerDialogDeps) ListProviders() []providersdialog.ProviderEntry {
	s := d.settings()
	active := ""
	if s != nil {
		active = s.ActiveProvider()
	}
	var out []providersdialog.ProviderEntry
	for id, def := range config.BuiltInProviders {
		canonical := string(id)
		out = append(out, providersdialog.ProviderEntry{
			ID:          canonical,
			DisplayID:   config.ProviderDisplayID(canonical),
			DisplayName: def.DisplayName,
			WireFormat:  def.WireFormat,
			IsActive:    canonical == active,
			Model:       d.modelFor(canonical),
		})
	}
	if s != nil && s.Providers != nil {
		for id, custom := range s.Providers.Custom {
			name := custom.DisplayName
			if name == "" {
				name = id
			}
			wf := custom.WireFormat
			if wf == "" {
				wf = config.WireFormatOpenAIChat
			}
			out = append(out, providersdialog.ProviderEntry{
				ID:          id,
				DisplayID:   id,
				DisplayName: name,
				WireFormat:  wf,
				IsCustom:    true,
				IsActive:    id == active,
				Model:       d.modelFor(id),
			})
		}
	}
	return out
}

func (d *providerDialogDeps) modelFor(id string) string {
	endpoint, err := provider.ResolveEndpointForProvider(d.settings(), id)
	if err != nil {
		return ""
	}
	return endpoint.Model
}

func (d *providerDialogDeps) ActiveProviderID() string {
	if d.settings() == nil {
		return ""
	}
	return d.settings().ActiveProvider()
}

func (d *providerDialogDeps) SwitchProvider(ctx context.Context, id string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.SaveActiveProvider(d.loader(), d.settings(), id); err != nil {
		return err
	}
	_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
	return err
}

func (d *providerDialogDeps) SetAPIKey(ctx context.Context, id, key string) error {
	if err := credentials.SetProviderAPIKey(ctx, id, key); err != nil {
		return err
	}
	if d.ActiveProviderID() == config.NormalizeProviderID(id) {
		_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
		return err
	}
	return nil
}

func (d *providerDialogDeps) AddCustomProvider(ctx context.Context, id string, def config.CustomProviderDefinition, apiKey string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.AddCustomProvider(d.settings(), id, def); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	if apiKey != "" {
		if err := credentials.SetProviderAPIKey(ctx, id, apiKey); err != nil {
			return fmt.Errorf("provider added but key save failed: %w", err)
		}
	}
	return nil
}

func (d *providerDialogDeps) RemoveCustomProvider(_ context.Context, id string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.RemoveCustomProvider(d.settings(), id); err != nil {
		return err
	}
	return d.loader().Save(d.settings())
}

func (d *providerDialogDeps) DiscoverModels(ctx context.Context, id string) ([]string, error) {
	endpoint, err := provider.ResolveEndpointForProvider(d.settings(), id)
	if err != nil {
		return nil, err
	}

	if endpoint.WireFormat == config.WireFormatGemini {
		apiKey := endpoint.Bearer
		if apiKey == "" && endpoint.RequiresAPIKey {
			if key, kerr := credentials.ResolveProviderAPIKey(ctx, endpoint.ProviderID); kerr == nil {
				apiKey = key
			}
		}
		infos, err := provider.DiscoverGeminiModels(ctx, apiKey, nil)
		if err != nil {
			return nil, err
		}
		return modelIDsFromInfos(infos), nil
	}

	if endpoint.BaseURL == "" {
		return nil, fmt.Errorf("provider %q has no endpoint URL to query", config.ProviderDisplayID(id))
	}
	bearer := endpoint.Bearer
	if bearer == "" && endpoint.RequiresAPIKey {
		if key, kerr := credentials.ResolveProviderAPIKey(ctx, endpoint.ProviderID); kerr == nil {
			bearer = key
		}
	}
	infos := provider.DiscoverModels(ctx, endpoint.BaseURL, bearer, nil)
	models := modelIDsFromInfos(infos)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned from %s (check base URL and API key)", config.ProviderDisplayID(id))
	}
	return models, nil
}

func modelIDsFromInfos(infos []provider.ModelInfo) []string {
	models := make([]string, 0, len(infos))
	for _, info := range infos {
		models = append(models, info.ID)
	}
	provider.SortModelIDs(models)
	return models
}

func (d *providerDialogDeps) SetModel(ctx context.Context, id, model string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.SetProviderModel(d.settings(), config.NormalizeProviderID(id), model); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, id)
}

// CurrentModel returns the provider's resolved live/default model id, used by the
// activation screen to keep the live model inside the curated active set.
func (d *providerDialogDeps) CurrentModel(id string) string {
	endpoint, err := provider.ResolveEndpointForProvider(d.settings(), id)
	if err != nil {
		return ""
	}
	return endpoint.Model
}

func (d *providerDialogDeps) ApplySetting(ctx context.Context, id, key, value string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.ApplyProviderSetting(d.settings(), id, key, value); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, id)
}

func (d *providerDialogDeps) UpdateCustomDefinition(ctx context.Context, id, field, value string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.UpdateCustomProviderDefinition(d.settings(), id, field, value); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, id)
}

func (d *providerDialogDeps) ProviderSettings(id string) map[string]string {
	values := provider.InstanceSettingValues(d.settings(), id)
	if s := d.settings(); s != nil && s.Providers != nil {
		if custom, ok := s.Providers.Custom[id]; ok {
			values["displayName"] = custom.DisplayName
			values["baseUrl"] = custom.BaseURL
			if custom.WireFormat != "" {
				values["wireFormat"] = string(custom.WireFormat)
			}
			values["apiKeyEnvVar"] = custom.APIKeyEnvVar
		}
	}
	return values
}

func (d *providerDialogDeps) ValidSettingKeys(id string) []string {
	var custom *config.CustomProviderDefinition
	if s := d.settings(); s != nil && s.Providers != nil {
		if c, ok := s.Providers.Custom[id]; ok {
			custom = &c
		}
	}
	return config.ValidSettingKeysForProvider(config.NormalizeProviderID(id), custom)
}

// ActiveModels returns the raw curated active-model set (no fallback) so the
// activation screen can pre-check the saved models.
func (d *providerDialogDeps) ActiveModels(id string) []string {
	return provider.CuratedActiveModels(d.settings(), id)
}

// SetActiveModels persists the curated active-model set. Activation does not
// change the live model, so no runner rebuild is required.
func (d *providerDialogDeps) SetActiveModels(_ context.Context, id string, models []string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.SetActiveModels(d.settings(), id, models); err != nil {
		return err
	}
	return d.loader().Save(d.settings())
}

func (d *providerDialogDeps) rebuildIfActive(ctx context.Context, id string) error {
	if d.ActiveProviderID() == config.NormalizeProviderID(id) {
		_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
		return err
	}
	return nil
}

// ModelsDialogDeps returns the side-effect adapter the /models picker uses.
func (a *App) ModelsDialogDeps() modelsdialog.Deps {
	return &modelsDialogDeps{app: a}
}

// modelsDialogDeps implements modelsdialog.Deps over the App's runner, loader,
// and settings. The /models picker is global: it lists every activated model
// across all configured providers and selecting one switches the active
// provider and its live model in a single step.
type modelsDialogDeps struct {
	app *App
}

func (d *modelsDialogDeps) settings() *config.Settings { return d.app.deps.Settings }
func (d *modelsDialogDeps) loader() *config.Loader     { return d.app.deps.Loader }

func (d *modelsDialogDeps) ActiveProviderID() string {
	if d.settings() == nil {
		return ""
	}
	return d.settings().ActiveProvider()
}

func (d *modelsDialogDeps) CurrentModel() string {
	id := d.ActiveProviderID()
	if id == "" {
		return ""
	}
	endpoint, err := provider.ResolveEndpointForProvider(d.settings(), id)
	if err != nil {
		return ""
	}
	return endpoint.Model
}

// ListActiveModels returns every activated (curated) model across all built-in
// and custom providers, plus the active provider's current model so the picker
// is never empty when something is in use.
func (d *modelsDialogDeps) ListActiveModels() []modelsdialog.ModelEntry {
	s := d.settings()
	if s == nil {
		return nil
	}
	var entries []modelsdialog.ModelEntry
	seen := map[string]bool{}
	add := func(id, model string) {
		model = strings.TrimSpace(model)
		if id == "" || model == "" {
			return
		}
		key := id + "\x00" + model
		if seen[key] {
			return
		}
		seen[key] = true
		entries = append(entries, modelsdialog.ModelEntry{
			ProviderID:    id,
			ProviderLabel: config.ProviderDisplayID(id),
			Model:         model,
		})
	}
	addProvider := func(id string) {
		for _, model := range provider.CuratedActiveModels(s, id) {
			add(id, model)
		}
	}
	for id := range config.BuiltInProviders {
		addProvider(string(id))
	}
	if s.Providers != nil {
		for id := range s.Providers.Custom {
			addProvider(id)
		}
	}
	if active := config.NormalizeProviderID(s.ActiveProvider()); active != "" {
		add(active, d.CurrentModel())
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ProviderLabel != entries[j].ProviderLabel {
			return entries[i].ProviderLabel < entries[j].ProviderLabel
		}
		return entries[i].Model < entries[j].Model
	})
	return entries
}

// SelectModel switches to providerID (if it differs from the active provider)
// and sets its live model, then rebuilds the runner.
func (d *modelsDialogDeps) SelectModel(ctx context.Context, id, model string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	id = config.NormalizeProviderID(id)
	if err := provider.SetProviderModel(d.settings(), id, model); err != nil {
		return err
	}
	if d.ActiveProviderID() != id {
		if err := provider.SaveActiveProvider(d.loader(), d.settings(), id); err != nil {
			return err
		}
	} else if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
	return err
}
