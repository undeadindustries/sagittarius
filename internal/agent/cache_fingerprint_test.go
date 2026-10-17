package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

func TestPrefixDetectorClassifications(t *testing.T) {
	t.Parallel()

	baseTools := []provider.ToolDeclaration{
		{Name: "read_file", Description: "Read a file"},
		{Name: "edit", Description: "Edit a file"},
	}
	msg0 := provider.Message{Role: provider.RoleUser, Parts: []provider.Part{{Text: "fix main.go"}}}
	msg1 := provider.Message{
		Role: provider.RoleModel,
		Parts: []provider.Part{{
			FunctionCall: &provider.ToolCall{
				ID:   "c1",
				Name: "read_file",
				Args: map[string]any{"file_path": "main.go", "offset": 1},
			},
		}},
	}
	msg2 := provider.Message{
		Role: provider.RoleUser,
		Parts: []provider.Part{{
			FunctionResponse: &provider.FunctionResponse{
				CallID:   "c1",
				Name:     "read_file",
				Response: map[string]any{"content": "package main\n"},
			},
		}},
	}

	t.Run("append-only-multi-round-has-zero-breaks", func(t *testing.T) {
		t.Parallel()
		var det prefixDetector

		obs1 := det.observe(&provider.GenerateRequest{
			SystemInstruction: "sys v1",
			Tools:             baseTools,
			Messages:          []provider.Message{msg0},
		})
		if obs1.Broken || obs1.BreakIndex != -1 {
			t.Fatalf("round 1 should never report a break: %+v", obs1)
		}

		obs2 := det.observe(&provider.GenerateRequest{
			SystemInstruction: "sys v1",
			Tools:             baseTools,
			Messages:          []provider.Message{msg0, msg1, msg2},
		})
		if obs2.Broken || obs2.BreakIndex != -1 {
			t.Fatalf("append-only round 2 should not report a break: %+v", obs2)
		}
	})

	t.Run("expected-vs-unexpected-breaks", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name           string
			expectedNote   string
			nextReq        *provider.GenerateRequest
			wantBroken     bool
			wantPos        int
			wantReason     string
			wantUnexpected bool
		}{
			{
				name: "unexpected-system-mutation",
				nextReq: &provider.GenerateRequest{
					SystemInstruction: "sys v2 mutated",
					Tools:             baseTools,
					Messages:          []provider.Message{msg0, msg1, msg2},
				},
				wantBroken:     true,
				wantPos:        0,
				wantReason:     "unexpected:system",
				wantUnexpected: true,
			},
			{
				name: "unexpected-tools-reorder",
				nextReq: &provider.GenerateRequest{
					SystemInstruction: "sys v1",
					Tools:             []provider.ToolDeclaration{baseTools[1], baseTools[0]},
					Messages:          []provider.Message{msg0, msg1, msg2},
				},
				wantBroken:     true,
				wantPos:        0,
				wantReason:     "unexpected:tools",
				wantUnexpected: true,
			},
			{
				name: "unexpected-mid-history-edit",
				nextReq: &provider.GenerateRequest{
					SystemInstruction: "sys v1",
					Tools:             baseTools,
					Messages: []provider.Message{
						msg0,
						msg1,
						{
							Role: provider.RoleUser,
							Parts: []provider.Part{{
								FunctionResponse: &provider.FunctionResponse{
									CallID:   "c1",
									Name:     "read_file",
									Response: map[string]any{"content": "masked unexpectedly"},
								},
							}},
						},
					},
				},
				wantBroken:     true,
				wantPos:        2,
				wantReason:     "unexpected:history",
				wantUnexpected: true,
			},
			{
				name:         "expected-masking",
				expectedNote: "masking",
				nextReq: &provider.GenerateRequest{
					SystemInstruction: "sys v1",
					Tools:             baseTools,
					Messages: []provider.Message{
						msg0,
						msg1,
						{
							Role: provider.RoleUser,
							Parts: []provider.Part{{
								FunctionResponse: &provider.FunctionResponse{
									CallID:   "c1",
									Name:     "read_file",
									Response: map[string]any{"content": "[masked]"},
								},
							}},
						},
					},
				},
				wantBroken:     true,
				wantPos:        2,
				wantReason:     "masking",
				wantUnexpected: false,
			},
			{
				name:         "expected-compression-shrinks-history",
				expectedNote: "compression",
				nextReq: &provider.GenerateRequest{
					SystemInstruction: "sys v2 rebuilt on compress",
					Tools:             baseTools,
					Messages: []provider.Message{
						{Role: provider.RoleUser, Parts: []provider.Part{{Text: "<state_snapshot>"}}},
					},
				},
				wantBroken:     true,
				wantPos:        0,
				wantReason:     "compression",
				wantUnexpected: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				var det prefixDetector
				det.observe(&provider.GenerateRequest{
					SystemInstruction: "sys v1",
					Tools:             baseTools,
					Messages:          []provider.Message{msg0, msg1, msg2},
				})
				if tt.expectedNote != "" {
					det.noteExpectedBreak(tt.expectedNote)
				}
				obs := det.observe(tt.nextReq)
				if obs.Broken != tt.wantBroken || obs.BreakIndex != tt.wantPos || obs.Reason != tt.wantReason || obs.Unexpected != tt.wantUnexpected {
					t.Errorf("got %+v, want broken=%v pos=%d reason=%q unexpected=%v",
						obs, tt.wantBroken, tt.wantPos, tt.wantReason, tt.wantUnexpected)
				}
			})
		}
	})
}

func TestRunnerMultiRoundPromptCacheStability(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sampleFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(sampleFile, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fg := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{
					ToolCalls: []provider.ToolCall{{
						ID:   "call-1",
						Name: "read_file",
						Args: map[string]any{"file_path": sampleFile},
					}},
					Usage: &provider.Usage{
						InputTokens:    1000,
						OutputTokens:   50,
						CachedTokens:   0,
						CachedReported: true,
					},
					Done: true,
				},
			},
			{
				{
					TextDelta: "Done reading main.go.",
					Usage: &provider.Usage{
						InputTokens:    1200,
						OutputTokens:   30,
						CachedTokens:   1000,
						CachedReported: true,
					},
					Done: true,
				},
			},
		},
	}

	assertCache := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Debug: &config.SagittariusDebugConfig{
				AssertPromptCache: &assertCache,
			},
		},
	}

	r, err := NewRunner(RunnerConfig{
		Generator:   fg,
		Model:       "qwen3-coder",
		Settings:    settings,
		WorkDir:     dir,
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events, err := r.RunTurn(testContext(t), "inspect main.go")
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	evs := collectEvents(t, events)
	for _, ev := range evs {
		if ev.Err != nil {
			t.Fatalf("unexpected stream error: %v", ev.Err)
		}
	}

	stats := r.Stats()
	if len(stats.CacheBreaks) != 0 {
		t.Fatalf("expected 0 cache breaks across 2-round tool turn, got %+v", stats.CacheBreaks)
	}
	if !stats.LastCacheReported {
		t.Errorf("LastCacheReported = false, want true")
	}
	if stats.LastInputTokens != 1200 || stats.LastCachedTokens != 1000 {
		t.Errorf("LastInputTokens=%d LastCachedTokens=%d, want 1200 / 1000",
			stats.LastInputTokens, stats.LastCachedTokens)
	}
	if len(stats.ModelUsage) != 1 || stats.ModelUsage[0].CachedTokens != 1000 || !stats.ModelUsage[0].CacheReported {
		t.Errorf("ModelUsage = %+v, want CachedTokens=1000 CacheReported=true", stats.ModelUsage)
	}
}

func TestRunnerAssertPromptCacheCatchesUnexpectedSystemBreak(t *testing.T) {
	t.Parallel()

	fg := &fakeGenerator{
		batches: [][]provider.StreamResponse{
			{
				{TextDelta: "first answer", Done: true},
			},
			{
				{TextDelta: "second answer", Done: true},
			},
		},
	}

	assertCache := true
	settings := &config.Settings{
		Sagittarius: &config.SagittariusSettings{
			Debug: &config.SagittariusDebugConfig{
				AssertPromptCache: &assertCache,
			},
		},
	}

	r, err := NewRunner(RunnerConfig{
		Generator:   fg,
		Model:       "qwen3-coder",
		Settings:    settings,
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	events1, err := r.RunTurn(testContext(t), "turn 1")
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	for _, ev := range collectEvents(t, events1) {
		if ev.Err != nil {
			t.Fatalf("turn 1 error: %v", ev.Err)
		}
	}

	// Simulate an unannotated system prompt mutation (bug without noteExpectedBreak).
	r.modelMu.Lock()
	r.system = "system v2 mutated behind prefixDetector's back"
	r.modelMu.Unlock()

	events2, err := r.RunTurn(testContext(t), "turn 2")
	if err != nil {
		t.Fatalf("turn 2 setup: %v", err)
	}
	var gotErr error
	for _, ev := range collectEvents(t, events2) {
		if ev.Err != nil {
			gotErr = ev.Err
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "prompt cache assertion failed") || !strings.Contains(gotErr.Error(), "unexpected:system") {
		t.Fatalf("expected assertPromptCache error on turn 2, got %v", gotErr)
	}
}
