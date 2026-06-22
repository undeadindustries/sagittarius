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
//
// recordFn, when non-nil, is called after each compression with the provider id,
// model id, mode, token counts, and optional cost so the runner can track
// summarizer usage in session metrics. Pass Runner.RecordUsage.
func NewContextManager(
	settings *config.Settings,
	gen provider.ContentGenerator,
	modelFn func() string,
	provFn func() string,
	modeFn func() string,
	sessionID string,
	recordFn func(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool),
) *contextmgmt.Manager {
	liveModel := ""
	if modelFn != nil {
		liveModel = modelFn()
	}
	cm := provider.ResolveContextManagement(settings, liveModel)
	if !cm.Enabled {
		return nil
	}

	var summarize contextmgmt.Summarizer
	if gen != nil {
		summarize = newProviderSummarizer(gen, modelFn, provFn, modeFn, recordFn)
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
// modelFn/provFn/modeFn are resolved per call so the summarizer follows
// mid-session changes (interaction-mode switches, provider rebuilds).
// recordFn, when non-nil, is called after the stream drains with real token
// counts (falling back to heuristics) so the caller can attribute compression
// costs to session metrics.
func newProviderSummarizer(
	gen provider.ContentGenerator,
	modelFn func() string,
	provFn func() string,
	modeFn func() string,
	recordFn func(prov, model, mode string, inTok, outTok int, costUSD float64, costKnown bool),
) contextmgmt.Summarizer {
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
		var streamUsage *provider.Usage
		for resp := range respCh {
			if resp.Error != nil {
				return "", fmt.Errorf("summarize: stream: %w", resp.Error)
			}
			if resp.TextDelta != "" {
				summary.WriteString(resp.TextDelta)
			}
			if resp.Usage != nil {
				streamUsage = resp.Usage
			}
		}

		if recordFn != nil {
			prov := ""
			if provFn != nil {
				prov = provFn()
			}
			mode := "agent"
			if modeFn != nil {
				mode = modeFn()
			}
			var inTok, outTok int
			var costUSD float64
			var costKnown bool
			if streamUsage != nil {
				inTok = streamUsage.InputTokens
				outTok = streamUsage.OutputTokens
				costUSD = streamUsage.CostUSD
				costKnown = streamUsage.CostKnown
			} else {
				inTok = estimateMessageTokens(req.Messages)
				outTok = contextmgmt.EstimateTokens([]provider.Part{{Text: summary.String()}})
			}
			recordFn(prov, model, mode, inTok, outTok, costUSD, costKnown)
		}

		return summary.String(), nil
	}
}
