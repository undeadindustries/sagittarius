package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/slash"
	"github.com/undeadindustries/sagittarius/internal/ui"
	"github.com/undeadindustries/sagittarius/internal/ui/modelpickdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modelsdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/modesdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/providersdialog"
	"github.com/undeadindustries/sagittarius/internal/ui/systempromptdialog"
)

// mapDialogKind translates a slash dialog request into the UI event kind.
func mapDialogKind(kind slash.DialogKind) ui.DialogKind {
	switch kind {
	case slash.DialogProviders:
		return ui.DialogProviders
	case slash.DialogModels:
		return ui.DialogModels
	case slash.DialogModelPick:
		return ui.DialogModelPick
	case slash.DialogModes:
		return ui.DialogModes
	case slash.DialogSystemPrompt:
		return ui.DialogSystemPrompt
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
		infos, err := discoverModelInfos(ctx, d.settings(), id)
		if err != nil {
			return nil, err
		}
		return modelIDsFromInfos(infos), nil
	}
	if endpoint.BaseURL == "" {
		return nil, fmt.Errorf("provider %q has no endpoint URL to query", config.ProviderDisplayID(id))
	}
	infos, err := discoverModelInfos(ctx, d.settings(), id)
	if err != nil {
		return nil, err
	}
	models := modelIDsFromInfos(infos)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models returned from %s (check base URL and API key)", config.ProviderDisplayID(id))
	}
	return models, nil
}

// discoverModelInfos resolves a provider's endpoint and queries it for the model
// list (including context limits). It is best-effort for the non-Gemini path and
// surfaces the Gemini error so callers can report a missing key.
func discoverModelInfos(ctx context.Context, settings *config.Settings, id string) ([]provider.ModelInfo, error) {
	endpoint, err := provider.ResolveEndpointForProvider(settings, id)
	if err != nil {
		return nil, err
	}
	resolveBearer := func() string {
		if endpoint.Bearer != "" {
			return endpoint.Bearer
		}
		if endpoint.RequiresAPIKey {
			if key, kerr := credentials.ResolveProviderAPIKey(ctx, endpoint.ProviderID); kerr == nil {
				return key
			}
		}
		return ""
	}
	if endpoint.WireFormat == config.WireFormatGemini {
		return provider.DiscoverGeminiModels(ctx, resolveBearer(), nil)
	}
	if endpoint.BaseURL == "" {
		return nil, nil
	}
	return provider.DiscoverModels(ctx, endpoint.BaseURL, resolveBearer(), nil), nil
}

// applyDiscoveredContextLimit best-effort sets a provider's contextLimit to the
// model's reported window when the user has not pinned it. It does not persist;
// the caller's Save flushes the mutation. Failures are ignored so a switch never
// blocks on discovery.
func applyDiscoveredContextLimit(ctx context.Context, settings *config.Settings, providerID, model string) {
	if settings == nil || strings.TrimSpace(model) == "" {
		return
	}
	limit := provider.StaticContextLimit(model)
	if limit == 0 {
		infos, _ := discoverModelInfos(ctx, settings, providerID)
		limit = provider.ContextLimitForModel(infos, model)
	}
	if limit > 0 {
		_, _ = provider.MaybeSetContextLimit(settings, providerID, limit)
	}
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
	applyDiscoveredContextLimit(ctx, d.settings(), id, model)
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

// EffectiveProviderSettings returns resolved display strings (overrides plus
// computed defaults) for the keys the edit sheet annotates.
func (d *providerDialogDeps) EffectiveProviderSettings(id string) map[string]string {
	out := map[string]string{}
	s := d.settings()
	if s == nil {
		return out
	}
	canonical := config.NormalizeProviderID(id)
	model := d.modelFor(id)
	inst := s.ProviderInstance(canonical)

	if t := config.ResolveEffectiveTemperature(s, canonical, model); t != nil {
		out["temperature"] = strconv.FormatFloat(*t, 'g', -1, 64)
	} else {
		out["temperature"] = "model decides"
	}

	variant := config.ResolveVariant(s, canonical, model)
	if inst != nil && inst.CompressionThreshold != nil {
		out["compressionThreshold"] = strconv.FormatFloat(*inst.CompressionThreshold, 'g', -1, 64)
	} else {
		out["compressionThreshold"] = strconv.FormatFloat(config.VariantCompressionThreshold(variant), 'g', -1, 64)
	}

	if inst != nil && inst.ContextLimit != nil {
		out["contextLimit"] = strconv.Itoa(*inst.ContextLimit)
	}

	enabled := "true"
	if inst != nil && inst.EnableTools != nil {
		enabled = strconv.FormatBool(*inst.EnableTools)
	}
	out["enableTools"] = enabled

	parsing := string(config.ToolCallParsingLenient)
	if inst != nil && inst.ToolCallParsing != "" {
		parsing = string(inst.ToolCallParsing)
	}
	out["toolCallParsing"] = parsing

	return out
}

// SystemPromptPresetID returns the preset id matching the provider's current
// personality + variant.
func (d *providerDialogDeps) SystemPromptPresetID(id string) string {
	return provider.CurrentSystemPromptPreset(d.settings(), id)
}

// ApplySystemPromptPreset writes the preset's personality/variant and returns an
// info line describing the suggested sampling knobs (and which were kept pinned).
func (d *providerDialogDeps) ApplySystemPromptPreset(ctx context.Context, id, presetID string) (string, error) {
	if d.loader() == nil || d.settings() == nil {
		return "", fmt.Errorf("settings not loaded")
	}
	res, err := provider.ApplySystemPromptPreset(d.settings(), id, presetID)
	if err != nil {
		return "", err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return "", err
	}
	if rerr := d.rebuildIfActive(ctx, id); rerr != nil {
		return "", rerr
	}
	return formatPresetInfo(res), nil
}

func formatPresetInfo(res provider.PresetApplyResult) string {
	temp := "left to the model"
	if res.DefaultTemperature != nil {
		temp = strconv.FormatFloat(*res.DefaultTemperature, 'g', -1, 64)
		if res.TemperaturePinned {
			temp += " (kept at your custom value)"
		}
	}
	comp := strconv.FormatFloat(res.CompressionThreshold, 'g', -1, 64)
	if res.CompressionPinned {
		comp += " (kept at your custom value)"
	}
	return fmt.Sprintf("System prompt → %s. Suggested temperature %s and compression threshold %s for generic models.",
		res.Label, temp, comp)
}

func (d *providerDialogDeps) ClearSetting(ctx context.Context, id, key string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.ClearProviderSetting(d.settings(), id, key); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, id)
}

func (d *providerDialogDeps) ResetSettings(ctx context.Context, id string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.ResetProviderInstanceOverrides(d.settings(), id); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, id)
}

func (d *providerDialogDeps) rebuildIfActive(ctx context.Context, id string) error {
	if d.ActiveProviderID() == config.NormalizeProviderID(id) {
		_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
		return err
	}
	return nil
}

// ModelsDialogDeps returns the side-effect adapter the /models per-model settings editor uses.
func (a *App) ModelsDialogDeps() modelsdialog.Deps {
	return &modelsDialogDeps{app: a}
}

// modelsDialogDeps implements modelsdialog.Deps: the per-model settings editor
// lists all active (provider, model) pairs globally and edits per-model overrides.
type modelsDialogDeps struct {
	app *App
}

func (d *modelsDialogDeps) settings() *config.Settings { return d.app.deps.Settings }
func (d *modelsDialogDeps) loader() *config.Loader     { return d.app.deps.Loader }

func (d *modelsDialogDeps) ListAllActiveModels() []modelsdialog.ModelEntry {
	s := d.settings()
	if s == nil {
		return nil
	}
	pairs := provider.AllActiveModels(s)
	entries := make([]modelsdialog.ModelEntry, 0, len(pairs))
	for _, p := range pairs {
		entries = append(entries, modelsdialog.ModelEntry{
			ProviderID:    p.ProviderID,
			ProviderLabel: p.DisplayID,
			Model:         p.Model,
		})
	}
	return entries
}

func (d *modelsDialogDeps) GetModelSettings(providerID, model string) map[string]string {
	if d.settings() == nil {
		return nil
	}
	return provider.ModelConfigValues(d.settings(), providerID, model)
}

func (d *modelsDialogDeps) SetModelSetting(ctx context.Context, providerID, model, key, value string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.SetModelConfig(d.settings(), providerID, model, key, value); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, providerID)
}

func (d *modelsDialogDeps) ClearModelSetting(ctx context.Context, providerID, model, key string) error {
	if d.loader() == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	if err := provider.ClearModelConfig(d.settings(), providerID, model, key); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	return d.rebuildIfActive(ctx, providerID)
}

// SystemPromptDialogDeps returns the adapter for the /system-prompt picker.
func (a *App) SystemPromptDialogDeps() systempromptdialog.Deps {
	return &systemPromptDialogDeps{app: a}
}

type systemPromptDialogDeps struct {
	app *App
}

func (d *systemPromptDialogDeps) CurrentPresetID() string {
	if d.app == nil || d.app.deps.Settings == nil {
		return ""
	}
	return config.ProjectSystemPromptPresetID(d.app.deps.Settings)
}

func (d *systemPromptDialogDeps) ApplyPreset(ctx context.Context, presetID string) (string, error) {
	if d.app == nil || d.app.deps.Hooks == nil {
		return "", fmt.Errorf("app not available")
	}
	return d.app.deps.Hooks.ApplyProjectSystemPromptPreset(ctx, presetID)
}

func (d *modelsDialogDeps) rebuildIfActive(ctx context.Context, providerID string) error {
	if d.settings() == nil {
		return nil
	}
	if config.NormalizeProviderID(d.settings().ActiveProvider()) == config.NormalizeProviderID(providerID) {
		_, _, err := d.app.deps.Hooks.RebuildRunner(ctx)
		return err
	}
	return nil
}

// ModelPickDialogDeps returns the side-effect adapter the /model global picker uses.
func (a *App) ModelPickDialogDeps() modelpickdialog.Deps {
	return &modelPickDialogDeps{app: a}
}

type modelPickDialogDeps struct {
	app *App
}

func (d *modelPickDialogDeps) settings() *config.Settings { return d.app.deps.Settings }

func (d *modelPickDialogDeps) AllActiveModels() []modelpickdialog.ModelEntry {
	s := d.settings()
	if s == nil {
		return nil
	}
	pairs := provider.AllActiveModels(s)
	entries := make([]modelpickdialog.ModelEntry, 0, len(pairs))
	for _, p := range pairs {
		entries = append(entries, modelpickdialog.ModelEntry{
			ProviderID:  p.ProviderID,
			DisplayID:   p.DisplayID,
			DisplayName: p.DisplayName,
			Model:       p.Model,
		})
	}
	return entries
}

func (d *modelPickDialogDeps) CurrentProviderID() string {
	if d.settings() == nil {
		return ""
	}
	return d.settings().ActiveProvider()
}

func (d *modelPickDialogDeps) CurrentModel() string {
	id := d.CurrentProviderID()
	if id == "" {
		return ""
	}
	endpoint, err := provider.ResolveEndpointForProvider(d.settings(), id)
	if err != nil {
		return ""
	}
	return endpoint.Model
}

func (d *modelPickDialogDeps) SelectCurrentModel(ctx context.Context, providerID, model string) error {
	if d.app.deps.Loader == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	_, err := d.app.deps.Hooks.SelectCurrentModel(ctx, providerID, model)
	return err
}

// ModesDialogDeps returns the side-effect adapter the /modes editor uses.
func (a *App) ModesDialogDeps() modesdialog.Deps {
	return &modesDialogDeps{app: a}
}

type modesDialogDeps struct {
	app *App
}

func (d *modesDialogDeps) settings() *config.Settings { return d.app.deps.Settings }

func (d *modesDialogDeps) ListModes() []modesdialog.ModeEntry {
	s := d.settings()
	var modes *config.SagittariusModes
	if s != nil && s.Sagittarius != nil {
		modes = s.Sagittarius.Modes
	}
	modeNames := []string{"agent", "plan", "ask", "debug"}
	entries := make([]modesdialog.ModeEntry, 0, len(modeNames))
	for _, name := range modeNames {
		prov, model := modeModeConfigValues(modes, name)
		entries = append(entries, modesdialog.ModeEntry{
			Mode:     name,
			Provider: prov,
			Model:    model,
		})
	}
	return entries
}

func modeModeConfigValues(modes *config.SagittariusModes, modeName string) (prov, model string) {
	if modes == nil {
		return "", ""
	}
	var mc *config.SagittariusModeConfig
	switch modeName {
	case "agent":
		mc = modes.Agent
	case "plan":
		mc = modes.Plan
	case "ask":
		mc = modes.Ask
	case "debug":
		mc = modes.Debug
	}
	if mc == nil {
		return "", ""
	}
	return mc.Provider, mc.Model
}

func (d *modesDialogDeps) AllActiveModels() []modesdialog.ModelEntry {
	s := d.settings()
	if s == nil {
		return nil
	}
	pairs := provider.AllActiveModels(s)
	entries := make([]modesdialog.ModelEntry, 0, len(pairs))
	for _, p := range pairs {
		entries = append(entries, modesdialog.ModelEntry{
			ProviderID: p.ProviderID,
			DisplayID:  p.DisplayID,
			Model:      p.Model,
		})
	}
	return entries
}

func (d *modesDialogDeps) SetModeOverride(_ context.Context, modeName, providerID, model string) error {
	if d.app.deps.Loader == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	s := d.settings()
	if s.Sagittarius == nil {
		s.Sagittarius = &config.SagittariusSettings{}
	}
	if s.Sagittarius.Modes == nil {
		s.Sagittarius.Modes = &config.SagittariusModes{}
	}
	mc := &config.SagittariusModeConfig{
		Provider: config.NormalizeProviderID(providerID),
		Model:    model,
	}
	switch modeName {
	case "agent":
		if s.Sagittarius.Modes.Agent != nil {
			mc.SystemPromptSuffix = s.Sagittarius.Modes.Agent.SystemPromptSuffix
			mc.Extra = s.Sagittarius.Modes.Agent.Extra
		}
		s.Sagittarius.Modes.Agent = mc
	case "plan":
		if s.Sagittarius.Modes.Plan != nil {
			mc.SystemPromptSuffix = s.Sagittarius.Modes.Plan.SystemPromptSuffix
			mc.Extra = s.Sagittarius.Modes.Plan.Extra
		}
		s.Sagittarius.Modes.Plan = mc
	case "ask":
		if s.Sagittarius.Modes.Ask != nil {
			mc.SystemPromptSuffix = s.Sagittarius.Modes.Ask.SystemPromptSuffix
			mc.Extra = s.Sagittarius.Modes.Ask.Extra
		}
		s.Sagittarius.Modes.Ask = mc
	case "debug":
		if s.Sagittarius.Modes.Debug != nil {
			mc.SystemPromptSuffix = s.Sagittarius.Modes.Debug.SystemPromptSuffix
			mc.Extra = s.Sagittarius.Modes.Debug.Extra
		}
		s.Sagittarius.Modes.Debug = mc
	default:
		return fmt.Errorf("unknown mode %q", modeName)
	}
	return d.app.deps.Loader.Save(s)
}

func (d *modesDialogDeps) ClearModeOverride(_ context.Context, modeName string) error {
	if d.app.deps.Loader == nil || d.settings() == nil {
		return fmt.Errorf("settings not loaded")
	}
	s := d.settings()
	if s.Sagittarius == nil || s.Sagittarius.Modes == nil {
		return nil
	}
	clearModeConfigOverride(s.Sagittarius.Modes, modeName)
	return d.app.deps.Loader.Save(s)
}

func clearModeConfigOverride(modes *config.SagittariusModes, modeName string) {
	switch modeName {
	case "agent":
		if modes.Agent != nil {
			modes.Agent.Provider = ""
			modes.Agent.Model = ""
			if modes.Agent.SystemPromptSuffix == "" && modes.Agent.Extra == nil {
				modes.Agent = nil
			}
		}
	case "plan":
		if modes.Plan != nil {
			modes.Plan.Provider = ""
			modes.Plan.Model = ""
			if modes.Plan.SystemPromptSuffix == "" && modes.Plan.Extra == nil {
				modes.Plan = nil
			}
		}
	case "ask":
		if modes.Ask != nil {
			modes.Ask.Provider = ""
			modes.Ask.Model = ""
			if modes.Ask.SystemPromptSuffix == "" && modes.Ask.Extra == nil {
				modes.Ask = nil
			}
		}
	case "debug":
		if modes.Debug != nil {
			modes.Debug.Provider = ""
			modes.Debug.Model = ""
			if modes.Debug.SystemPromptSuffix == "" && modes.Debug.Extra == nil {
				modes.Debug = nil
			}
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
