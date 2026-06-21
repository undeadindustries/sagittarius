package agent

import (
	"context"
	"fmt"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
)

// mapDialogKind translates a slash dialog request into the UI event kind.
func mapDialogKind(kind slash.DialogKind) ui.DialogKind {
	switch kind {
	case slash.DialogProviders:
		return ui.DialogProviders
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
	models := make([]string, 0, len(infos))
	for _, info := range infos {
		models = append(models, info.ID)
	}
	return models, nil
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

func (d *providerDialogDeps) rebuildIfActive(ctx context.Context, id string) error {
	if d.ActiveProviderID() == config.NormalizeProviderID(id) {
		_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
		return err
	}
	return nil
}
