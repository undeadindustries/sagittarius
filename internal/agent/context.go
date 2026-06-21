package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/tools"
)

const (
	// reservedResponseTokens reserves headroom for the model reply when the
	// pre-turn budget layer projects next-turn usage (fork preTurnBudget).
	reservedResponseTokens = 8_192
	// proactiveCompressAt is the projected-usage fraction that forces an early
	// compression before the turn (fork preTurnBudget threshold).
	proactiveCompressAt = 0.85
	// ejectionMinAgeTurns is the minimum age (turns from the end) before a
	// write_file call is eligible for ejection (fork
	// DEFAULT_LOCAL_WRITE_FILE_EJECTION_MIN_AGE_TURNS).
	ejectionMinAgeTurns = 1
	// ejectionMinTokensPerCall skips write_file payloads below this estimated
	// token count so trivial writes are not ejected (fork
	// DEFAULT_LOCAL_WRITE_FILE_EJECTION_MIN_TOKENS_PER_CALL).
	ejectionMinTokensPerCall = 200
)

// NewContextManager builds the Phase 11 context manager for the active provider.
//
// It returns nil unless the active provider uses the openai-chat wire format
// (the fork's isLocalMode). Gemini-native and openai-responses paths are never
// masked or compressed client-side (AD-014/AD-015), so the runner treats a nil
// manager as a pure pass-through.
//
// Compression uses the active provider model only (no secondary/per-utility
// routing); a nil generator disables compression while masking and ejection
// still run.
//
// modelFn supplies the model id at summarization time rather than capturing it
// once, so compression tracks the live model: passing Runner.CompressionModel
// keeps the summarizer aligned with user turns across mode switches and honors a
// sagittarius.compression.model override (AD-015 active-model rule, AD-022).
func NewContextManager(
	settings *config.Settings,
	gen provider.ContentGenerator,
	modelFn func() string,
	sessionID string,
) *contextmgmt.Manager {
	cm := provider.ResolveContextManagement(settings)
	if !cm.Enabled {
		return nil
	}

	var summarize contextmgmt.Summarizer
	if gen != nil {
		summarize = newProviderSummarizer(gen, modelFn)
	}

	return contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:                     true,
		ContextLimit:                cm.ContextLimit,
		SessionID:                   sessionID,
		MaskingEnabled:              cm.MaskingEnabled,
		MaskingProtectionFraction:   cm.MaskingProtectionFraction,
		MaskingPrunableFraction:     cm.MaskingPrunableFraction,
		MaskingProtectLatestTurn:    cm.MaskingProtectLatestTurn,
		EjectionEnabled:             true,
		EjectionMinAgeTurns:         ejectionMinAgeTurns,
		EjectionMinTokensPerCall:    ejectionMinTokensPerCall,
		BudgetEnabled:               true,
		ReservedResponseTokens:      reservedResponseTokens,
		ProactiveCompressAt:         proactiveCompressAt,
		AdaptiveEnabled:             true,
		CompressionThreshold:        cm.CompressionThreshold,
		CompressionThresholdUserSet: cm.CompressionThresholdUserSet,
		PreserveFraction:            cm.PreserveFraction,
		WriteFileToolName:           tools.WriteFileToolName,
		ShellToolName:               tools.ShellToolName,
		Summarize:                   summarize,
	})
}

// newProviderSummarizer adapts a ContentGenerator into a contextmgmt.Summarizer
// that drives one non-streaming summarization turn on the active model. It
// drains the stream, concatenating text deltas and ignoring any tool calls.
// modelFn is resolved per call so the summarizer follows mid-session model
// changes (interaction-mode switches); a nil modelFn yields an empty model id,
// letting the provider apply its own default.
func newProviderSummarizer(gen provider.ContentGenerator, modelFn func() string) contextmgmt.Summarizer {
	return func(ctx context.Context, contents []contextmgmt.Message, systemInstruction string) (string, error) {
		var model string
		if modelFn != nil {
			model = modelFn()
		}
		req := &provider.GenerateRequest{
			Model:             model,
			SystemInstruction: systemInstruction,
			Messages:          append([]provider.Message(nil), contents...),
		}
		respCh, err := gen.GenerateContentStream(ctx, req)
		if err != nil {
			return "", fmt.Errorf("summarize: start stream: %w", err)
		}

		var summary strings.Builder
		for resp := range respCh {
			if resp.Error != nil {
				return "", fmt.Errorf("summarize: stream: %w", resp.Error)
			}
			if resp.TextDelta != "" {
				summary.WriteString(resp.TextDelta)
			}
		}
		return summary.String(), nil
	}
}
