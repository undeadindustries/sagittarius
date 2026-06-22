// Package e2e contains end-to-end tests that drive the compiled sagittarius
// binary as a subprocess.
//
// Live mode (default): scenarios run against real providers using cheap models,
// rotating across whatever credentials are available (Gemini, OpenAI, OpenAI
// Responses). Providers without a usable API key are skipped, not failed; a
// suite with zero usable providers skips entirely so `go test ./...` stays green
// without keys. Run the live suite explicitly with `make e2e`.
//
// Mock mode (SAGITTARIUS_E2E_MOCK=1): the same scenario table runs against an
// in-process openai-chat mock server with deterministic tool-call SSE, so the
// wiring is exercised without any API keys or network access.
//
// Model overrides (live mode) keep costs controllable:
//
//	SAGITTARIUS_E2E_MODEL_GEMINI
//	SAGITTARIUS_E2E_MODEL_OPENAI
//	SAGITTARIUS_E2E_MODEL_OPENAI_RESPONSES
package e2e

import (
	"context"
	"os"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// providerSpec is a static cheap-model entry for a built-in provider.
type providerSpec struct {
	id           string // settings.json provider id (GEMINI_PROVIDER value)
	wire         config.WireFormat
	defaultModel string // low-cost model used unless overridden
	modelEnv     string // env var overriding defaultModel
}

// builtinSpecs lists the built-in providers the live harness probes, each with a
// low-cost default model. OpenAI and OpenAI Responses share OPENAI_API_KEY.
var builtinSpecs = []providerSpec{
	{id: string(config.BuiltInGeminiAPIKey), wire: config.WireFormatGemini, defaultModel: "gemini-2.0-flash", modelEnv: "SAGITTARIUS_E2E_MODEL_GEMINI"},
	{id: string(config.BuiltInOpenAI), wire: config.WireFormatOpenAIChat, defaultModel: "gpt-4o-mini", modelEnv: "SAGITTARIUS_E2E_MODEL_OPENAI"},
	{id: string(config.BuiltInOpenAIResponses), wire: config.WireFormatOpenAIResponses, defaultModel: "gpt-5-mini", modelEnv: "SAGITTARIUS_E2E_MODEL_OPENAI_RESPONSES"},
}

func (s providerSpec) model() string {
	if v := strings.TrimSpace(os.Getenv(s.modelEnv)); v != "" {
		return v
	}
	return s.defaultModel
}

// liveProvider is a discovered provider with a usable key and resolved model.
type liveProvider struct {
	id    string
	model string
	wire  config.WireFormat
}

// discoverLiveProviders returns the built-in providers that have a usable API
// key (resolved via env, OS keychain, or the encrypted file fallback). This uses
// the same credential resolution path as the binary, so discovery matches what
// the subprocess will see when it inherits the current environment.
func discoverLiveProviders(ctx context.Context) []liveProvider {
	var out []liveProvider
	for _, s := range builtinSpecs {
		key, err := credentials.ResolveProviderAPIKey(ctx, s.id)
		if err != nil || strings.TrimSpace(key) == "" {
			continue
		}
		out = append(out, liveProvider{id: s.id, model: s.model(), wire: s.wire})
	}
	return out
}
