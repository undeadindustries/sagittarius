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
func NewContextManager(
	settings *config.Settings,
	gen provider.ContentGenerator,
	model, sessionID string,
) *contextmgmt.Manager {
	cm := provider.ResolveContextManagement(settings)
	if !cm.Enabled {
		return nil
	}

	var summarize contextmgmt.Summarizer
	if gen != nil {
		summarize = newProviderSummarizer(gen, model)
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
func newProviderSummarizer(gen provider.ContentGenerator, model string) contextmgmt.Summarizer {
	return func(ctx context.Context, contents []contextmgmt.Message, systemInstruction string) (string, error) {
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
