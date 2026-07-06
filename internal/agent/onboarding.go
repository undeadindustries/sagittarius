package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui/onboardingdialog"
)

const geminiProviderID = string(config.BuiltInGeminiAPIKey)

// OnboardingDialogDeps returns the adapter the first-run onboarding overlay uses.
func (a *App) OnboardingDialogDeps() onboardingdialog.Deps {
	return &onboardingDeps{inner: &providerDialogDeps{app: a}}
}

type onboardingDeps struct {
	inner *providerDialogDeps
}

func (d *onboardingDeps) settings() *config.Settings { return d.inner.settings() }
func (d *onboardingDeps) loader() *config.Loader     { return d.inner.loader() }

func (d *onboardingDeps) PrepareGemini(ctx context.Context, apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}
	if err := credentials.SetProviderAPIKey(ctx, geminiProviderID, apiKey); err != nil {
		return "", err
	}
	if err := provider.SaveActiveProvider(d.loader(), d.settings(), geminiProviderID); err != nil {
		return "", err
	}
	return geminiProviderID, nil
}

func (d *onboardingDeps) PreparePreset(ctx context.Context, presetID, apiKey string) (string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}
	id, err := ensurePresetProvider(d.settings(), presetID)
	if err != nil {
		return "", err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return "", err
	}
	if err := credentials.SetProviderAPIKey(ctx, id, apiKey); err != nil {
		return "", err
	}
	if err := provider.SaveActiveProvider(d.loader(), d.settings(), id); err != nil {
		return "", err
	}
	return id, nil
}

func (d *onboardingDeps) PrepareCustom(ctx context.Context, baseURL, apiKey string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	if baseURL == "" {
		return "", fmt.Errorf("base URL is required")
	}
	if apiKey == "" {
		return "", fmt.Errorf("API key is required")
	}
	id := claimCustomProviderID(d.settings(), baseURL)
	def := config.CustomProviderDefinition{
		DisplayName: "Custom",
		BaseURL:     baseURL,
		WireFormat:  config.WireFormatOpenAIChat,
	}
	if err := provider.AddCustomProvider(d.settings(), id, def); err != nil {
		return "", err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return "", err
	}
	if err := credentials.SetProviderAPIKey(ctx, id, apiKey); err != nil {
		return "", fmt.Errorf("provider added but key save failed: %w", err)
	}
	if err := provider.SaveActiveProvider(d.loader(), d.settings(), id); err != nil {
		return "", err
	}
	return id, nil
}

func (d *onboardingDeps) DiscoverModels(ctx context.Context, providerID string) ([]string, error) {
	return d.inner.DiscoverModels(ctx, providerID)
}

func (d *onboardingDeps) CompleteSetup(ctx context.Context, providerID, model string) error {
	providerID = config.NormalizeProviderID(providerID)
	model = strings.TrimSpace(model)
	if providerID == "" || model == "" {
		return fmt.Errorf("provider and model are required")
	}
	if err := provider.SetProviderModel(d.settings(), providerID, model); err != nil {
		return err
	}
	if err := provider.SetActiveModels(d.settings(), providerID, []string{model}); err != nil {
		return err
	}
	if err := d.loader().Save(d.settings()); err != nil {
		return err
	}
	if err := provider.SaveActiveProvider(d.loader(), d.settings(), providerID); err != nil {
		return err
	}
	_, _, err := d.inner.app.deps.Hooks.RebuildRunner(ctx)
	return err
}

// ensurePresetProvider materializes a config.ProviderPreset into a
// providers.custom.<id> entry (reusing the preset id) if one does not already
// exist, returning the provider id to use.
func ensurePresetProvider(settings *config.Settings, presetID string) (string, error) {
	if settings == nil {
		return "", fmt.Errorf("settings not loaded")
	}
	p, ok := config.LookupProviderPreset(presetID)
	if !ok {
		return "", fmt.Errorf("unknown provider preset %q", presetID)
	}
	if settings.Providers == nil {
		settings.Providers = &config.ProvidersSettings{}
	}
	if settings.Providers.Custom == nil {
		settings.Providers.Custom = map[string]config.CustomProviderDefinition{}
	}
	if _, exists := settings.Providers.Custom[p.ID]; !exists {
		settings.Providers.Custom[p.ID] = p.ToCustomProviderDefinition()
	}
	return p.ID, nil
}

func claimCustomProviderID(settings *config.Settings, baseURL string) string {
	return provider.ClaimCustomProviderID(settings, baseURL)
}
