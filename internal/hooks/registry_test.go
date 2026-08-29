package hooks_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/hooks"
)

func TestRegistry_LoadAndList(t *testing.T) {
	t.Parallel()
	globalHome := t.TempDir()
	projectRoot := t.TempDir()

	reg := hooks.NewRegistry(globalHome, projectRoot, filepath.Join(projectRoot, "docs", "plans"))

	globalCfg := &config.HooksConfig{
		Events: map[string][]config.HookDefConfig{
			"BeforeAgent": {
				{
					Matcher: "*",
					Hooks: []config.HookExecConfig{
						{Name: "global-hook", Command: "echo global"},
					},
				},
			},
		},
	}
	projectCfg := &config.HooksConfig{
		Events: map[string][]config.HookDefConfig{
			"BeforeTool": {
				{
					Matcher: "write_file",
					Hooks: []config.HookExecConfig{
						{Name: "proj-hook", Command: "echo proj"},
					},
				},
			},
		},
	}

	reg.LoadConfig(globalCfg, projectCfg)
	list := reg.ListHooks()

	if len(list) != 2 {
		t.Fatalf("expected 2 hooks listed, got %d", len(list))
	}

	// Trust project hook
	projHookConfig := hooks.HookConfig{
		Name:    "proj-hook",
		Command: "echo proj",
		Source:  hooks.SourceProject,
	}
	if err := reg.TrustManager().TrustHook(projectRoot, projHookConfig); err != nil {
		t.Fatalf("TrustHook failed: %v", err)
	}

	// Reload config and verify trusted state
	reg.LoadConfig(globalCfg, projectCfg)
	list2 := reg.ListHooks()
	foundProj := false
	for _, h := range list2 {
		if h.Name == "proj-hook" {
			foundProj = true
			if !h.Trusted {
				t.Error("expected proj-hook to be trusted")
			}
		}
	}
	if !foundProj {
		t.Error("proj-hook not found in listed hooks")
	}
}

func TestRegistry_TrustFingerprintChange(t *testing.T) {
	t.Parallel()
	globalHome := t.TempDir()
	projectRoot := t.TempDir()

	tm := hooks.NewTrustManager(globalHome)

	hookV1 := hooks.HookConfig{
		Name:    "my-hook",
		Command: "echo version1",
		Source:  hooks.SourceProject,
	}

	if tm.IsTrusted(projectRoot, hookV1) {
		t.Error("expected untrusted initial project hook")
	}

	if err := tm.TrustHook(projectRoot, hookV1); err != nil {
		t.Fatalf("TrustHook failed: %v", err)
	}

	if !tm.IsTrusted(projectRoot, hookV1) {
		t.Error("expected trusted project hook v1")
	}

	// Change command -> fingerprint changes
	hookV2 := hooks.HookConfig{
		Name:    "my-hook",
		Command: "echo version2_tampered",
		Source:  hooks.SourceProject,
	}

	if tm.IsTrusted(projectRoot, hookV2) {
		t.Error("expected modified project hook command to be untrusted")
	}
}

func TestRegistry_EnableDisable(t *testing.T) {
	t.Parallel()
	globalHome := t.TempDir()
	projectRoot := t.TempDir()

	reg := hooks.NewRegistry(globalHome, projectRoot, filepath.Join(projectRoot, "docs", "plans"))

	globalCfg := &config.HooksConfig{
		Events: map[string][]config.HookDefConfig{
			"BeforeAgent": {
				{
					Matcher: "*",
					Hooks: []config.HookExecConfig{
						{Name: "hook-a", Command: fakeHookCommand("json_allow")},
					},
				},
			},
		},
	}
	reg.LoadConfig(globalCfg, nil)

	if !reg.IsEnabled() {
		t.Error("expected registry to be globally enabled")
	}

	reg.DisableHook("hook-a")

	input := hooks.NewHookInput("sess-1", "/tmp/transcript.jsonl", projectRoot, hooks.EventBeforeAgent, 1)
	res, err := reg.FireEvent(context.Background(), hooks.EventBeforeAgent, "", input)
	if err != nil {
		t.Fatalf("FireEvent failed: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("expected 0 results when hook-a is disabled, got %d", len(res))
	}

	reg.EnableHook("hook-a")
	res2, err := reg.FireEvent(context.Background(), hooks.EventBeforeAgent, "", input)
	if err != nil {
		t.Fatalf("FireEvent failed: %v", err)
	}
	if len(res2) != 1 {
		t.Errorf("expected 1 result when hook-a is re-enabled, got %d", len(res2))
	}
}

func TestRegistry_TrustNamedAndSkipMessage(t *testing.T) {
	t.Parallel()
	globalHome := t.TempDir()
	projectRoot := t.TempDir()
	reg := hooks.NewRegistry(globalHome, projectRoot, filepath.Join(projectRoot, "docs", "plans"))
	reg.LoadConfig(nil, &config.HooksConfig{
		Events: map[string][]config.HookDefConfig{
			"AfterAgent": {{
				Matcher: "*",
				Hooks:   []config.HookExecConfig{{Name: "proj-hook", Command: "echo proj"}},
			}},
		},
	})

	input := hooks.NewHookInput("sess-1", "/tmp/transcript.jsonl", projectRoot, hooks.EventAfterAgent, 1)
	res, err := reg.FireEvent(context.Background(), hooks.EventAfterAgent, "", input)
	if err != nil {
		t.Fatalf("FireEvent: %v", err)
	}
	if len(res) != 1 || res[0].Error == nil {
		t.Fatalf("want one untrusted skip, got %+v", res)
	}
	if !strings.Contains(res[0].Error.Error(), "/hooks trust proj-hook") {
		t.Fatalf("skip error = %v, want it to name /hooks trust", res[0].Error)
	}

	if err := reg.TrustNamed("proj-hook"); err != nil {
		t.Fatalf("TrustNamed: %v", err)
	}
	list := reg.ListHooks()
	if len(list) != 1 || !list[0].Trusted {
		t.Fatalf("after TrustNamed, list = %+v", list)
	}

	if err := reg.TrustNamed("missing"); err == nil {
		t.Fatal("TrustNamed(missing) should error")
	}
}
