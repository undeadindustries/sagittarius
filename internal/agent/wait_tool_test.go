package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestAdmitWaitCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd           string
		wantConfirm   bool
		wantErrSubstr string
	}{
		{cmd: "test -f /tmp/done", wantConfirm: false},
		{cmd: "[ -f /tmp/done ]", wantConfirm: false},
		{cmd: "systemctl is-active nginx", wantConfirm: false},
		{cmd: "curl -sf http://127.0.0.1:8080/health", wantConfirm: false},
		{cmd: "pgrep -x mysqld", wantConfirm: false},
		{cmd: "rm -rf /tmp/x", wantErrSubstr: "dangerous"},
		{cmd: "systemctl restart nginx", wantErrSubstr: "mutating"},
		{cmd: "kill -0 1234", wantErrSubstr: "mutating"},
		{cmd: "some-unknown-bin --probe", wantConfirm: true},
		{cmd: "", wantErrSubstr: "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			confirm, err := admitWaitCommand(tc.cmd)
			if tc.wantErrSubstr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
					t.Fatalf("admitWaitCommand(%q) err = %v, want substring %q", tc.cmd, err, tc.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("admitWaitCommand(%q): %v", tc.cmd, err)
			}
			if confirm != tc.wantConfirm {
				t.Fatalf("admitWaitCommand(%q) confirm = %v, want %v", tc.cmd, confirm, tc.wantConfirm)
			}
		})
	}
}

func TestClampWaitTiming(t *testing.T) {
	t.Parallel()
	interval, timeout := clampWaitTiming(0, 0)
	if interval != defaultWaitInterval || timeout != defaultWaitTimeout {
		t.Fatalf("defaults = %s/%s, want %s/%s", interval, timeout, defaultWaitInterval, defaultWaitTimeout)
	}
	interval, timeout = clampWaitTiming(1, 10)
	if interval != minWaitInterval {
		t.Fatalf("min interval = %s, want %s", interval, minWaitInterval)
	}
	if timeout != 10*time.Second {
		t.Fatalf("timeout = %s, want 10s", timeout)
	}
	interval, timeout = clampWaitTiming(60, 10)
	if interval != timeout {
		t.Fatalf("interval %s should clamp to timeout %s", interval, timeout)
	}
	_, timeout = clampWaitTiming(10, 99999)
	if timeout != maxWaitTimeout {
		t.Fatalf("timeout = %s, want max %s", timeout, maxWaitTimeout)
	}
}

func TestWaitUntilReadyFirstCheck(t *testing.T) {
	t.Parallel()
	slept := false
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			return 0, "ready", nil
		},
		sleep: func(context.Context, time.Duration) error {
			slept = true
			return nil
		},
	}
	got, err := tool.executeStreamFor(testContext(t), map[string]any{
		tools.WaitUntilParamCommand: "test -f done",
	}, nil, false, nil, time.Millisecond, time.Millisecond, 50*time.Millisecond, time.Second)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if got["status"] != "ready" {
		t.Fatalf("status = %v, want ready", got["status"])
	}
	if got["checks"] != 1 {
		t.Fatalf("checks = %v, want 1", got["checks"])
	}
	if slept {
		t.Fatal("first-check success must not sleep")
	}
}

func TestWaitUntilTimeoutCarriesLastOutput(t *testing.T) {
	t.Parallel()
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			return 7, "not yet", nil
		},
		sleep: func(ctx context.Context, _ time.Duration) error {
			return sleepCtx(ctx, time.Millisecond)
		},
	}
	_, err := tool.executeStreamFor(testContext(t), map[string]any{
		tools.WaitUntilParamCommand: "test -f done",
	}, nil, false, nil, time.Millisecond, time.Millisecond, 15*time.Millisecond, time.Second)
	var te *tools.ToolError
	if !errors.As(err, &te) || te.Code != tools.ErrCodeExecutionTimeout {
		t.Fatalf("err = %v, want EXECUTION_TIMEOUT", err)
	}
	if !strings.Contains(te.Message, "not yet") {
		t.Fatalf("timeout message missing last output: %s", te.Message)
	}
	if te.Details["status"] != "timeout" {
		t.Fatalf("details status = %v", te.Details["status"])
	}
}

func TestWaitUntilCancelReturnsPromptly(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			return 1, "no", nil
		},
		sleep: func(ctx context.Context, _ time.Duration) error {
			cancel()
			return sleepCtx(ctx, time.Hour)
		},
	}
	started := time.Now()
	_, err := tool.executeStreamFor(ctx, map[string]any{
		tools.WaitUntilParamCommand: "test -f done",
	}, nil, false, nil, time.Millisecond, time.Millisecond, time.Hour, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("cancel took %s, want prompt return", time.Since(started))
	}
}

func TestWaitUntilUnknownDeniedNonInteractive(t *testing.T) {
	t.Parallel()
	tool := newWaitUntilTool(t.TempDir())
	_, err := tool.ExecuteStream(testContext(t), map[string]any{
		tools.WaitUntilParamCommand: "mystery-probe --ok",
	}, nil)
	var te *tools.ToolError
	if !errors.As(err, &te) || te.Code != tools.ErrCodeInvalidArgs {
		t.Fatalf("err = %v, want INVALID_ARGS", err)
	}
	if !strings.Contains(te.Message, "not recognized as read-only") {
		t.Fatalf("message = %s", te.Message)
	}
}

func TestWaitUntilUnknownConfirmInteractive(t *testing.T) {
	t.Parallel()
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			return 0, "ok", nil
		},
	}
	var mu sync.Mutex
	var gotConfirm string
	emit := func(ev ui.StreamEvent) {
		if ev.Type != ui.StreamToolConfirm {
			return
		}
		mu.Lock()
		gotConfirm = ev.Text
		mu.Unlock()
		ev.ConfirmReply <- ui.ConfirmOnce
	}
	got, err := tool.ExecuteInteractive(testContext(t), map[string]any{
		tools.WaitUntilParamCommand: "mystery-probe --ok",
	}, true, emit)
	if err != nil {
		t.Fatalf("ExecuteInteractive: %v", err)
	}
	if got["status"] != "ready" {
		t.Fatalf("status = %v", got["status"])
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(gotConfirm, "mystery-probe") {
		t.Fatalf("confirm text = %q", gotConfirm)
	}
}

func TestWaitUntilYoloSkipsUnknownConfirm(t *testing.T) {
	t.Parallel()
	for _, interactive := range []bool{true, false} {
		t.Run(boolName(interactive), func(t *testing.T) {
			t.Parallel()
			var ran string
			tool := &waitUntilTool{
				runCheck: func(_ context.Context, command string) (int, string, error) {
					ran = command
					return 0, "ok", nil
				},
			}
			ctx := tools.WithConfirmGate(testContext(t), tools.NewConfirmGate(true, nil, nil))
			got, err := tool.ExecuteInteractive(ctx, map[string]any{
				tools.WaitUntilParamCommand: "mystery-probe --ok",
			}, interactive, func(ev ui.StreamEvent) {
				if ev.Type == ui.StreamToolConfirm {
					t.Error("yolo must not prompt for an unclassified wait_until command")
				}
			})
			if err != nil {
				t.Fatalf("ExecuteInteractive: %v", err)
			}
			if got["status"] != "ready" {
				t.Fatalf("status = %v, want ready", got["status"])
			}
			if ran != "mystery-probe --ok" {
				t.Fatalf("check ran %q", ran)
			}
		})
	}
}

func TestWaitUntilYoloStillRejectsMutatingAndDangerous(t *testing.T) {
	t.Parallel()
	ctx := tools.WithConfirmGate(testContext(t), tools.NewConfirmGate(true, nil, nil))
	cases := []struct {
		cmd  string
		want string
	}{
		{cmd: "rm -f x", want: "dangerous"},
		{cmd: "systemctl restart nginx", want: "mutating"},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			t.Parallel()
			called := false
			tool := &waitUntilTool{
				runCheck: func(context.Context, string) (int, string, error) {
					called = true
					return 0, "", nil
				},
			}
			_, err := tool.executeStreamFor(ctx, map[string]any{
				tools.WaitUntilParamCommand: tc.cmd,
			}, nil, true, func(ui.StreamEvent) {
				t.Error("a rejected command must not prompt")
			}, time.Millisecond, time.Millisecond, time.Second, time.Second)
			var te *tools.ToolError
			if !errors.As(err, &te) || te.Code != tools.ErrCodeInvalidArgs {
				t.Fatalf("err = %v, want INVALID_ARGS", err)
			}
			if !strings.Contains(te.Message, tc.want) {
				t.Fatalf("message = %s, want substring %q", te.Message, tc.want)
			}
			if called {
				t.Fatal("rejected command must not run")
			}
		})
	}
}

func TestWaitUntilSessionGrantSkipsSecondPrompt(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	grants := map[string]bool{}
	ctx := tools.WithConfirmGate(testContext(t), tools.NewConfirmGate(false,
		func(name string) bool {
			mu.Lock()
			defer mu.Unlock()
			return grants[name]
		},
		func(name string) {
			mu.Lock()
			defer mu.Unlock()
			grants[name] = true
		},
	))
	checks := 0
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			checks++
			return 0, "ok", nil
		},
	}
	prompts := 0
	emit := func(ev ui.StreamEvent) {
		if ev.Type != ui.StreamToolConfirm {
			return
		}
		prompts++
		if prompts > 1 {
			t.Error("second wait_until must not prompt after a session grant")
			return
		}
		ev.ConfirmReply <- ui.ConfirmSession
	}
	args := map[string]any{tools.WaitUntilParamCommand: "mystery-probe --ok"}
	for range 2 {
		got, err := tool.ExecuteInteractive(ctx, args, true, emit)
		if err != nil {
			t.Fatalf("ExecuteInteractive: %v", err)
		}
		if got["status"] != "ready" {
			t.Fatalf("status = %v, want ready", got["status"])
		}
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want 1", prompts)
	}
	if checks != 2 {
		t.Fatalf("checks = %d, want 2", checks)
	}
	mu.Lock()
	defer mu.Unlock()
	if !grants[tools.WaitUntilToolName] {
		t.Fatalf("grants = %v, want %s", grants, tools.WaitUntilToolName)
	}
}

func TestWaitUntilConfirmOnceStillPromptsAgain(t *testing.T) {
	t.Parallel()
	granted := false
	ctx := tools.WithConfirmGate(testContext(t), tools.NewConfirmGate(false,
		func(string) bool { return false },
		func(string) { granted = true },
	))
	tool := &waitUntilTool{
		runCheck: func(context.Context, string) (int, string, error) {
			return 0, "ok", nil
		},
	}
	prompts := 0
	emit := func(ev ui.StreamEvent) {
		if ev.Type != ui.StreamToolConfirm {
			return
		}
		prompts++
		ev.ConfirmReply <- ui.ConfirmOnce
	}
	args := map[string]any{tools.WaitUntilParamCommand: "mystery-probe --ok"}
	for range 2 {
		if _, err := tool.ExecuteInteractive(ctx, args, true, emit); err != nil {
			t.Fatalf("ExecuteInteractive: %v", err)
		}
	}
	if prompts != 2 {
		t.Fatalf("prompts = %d, want 2", prompts)
	}
	if granted {
		t.Fatal("ConfirmOnce must not record a session grant")
	}
}

func boolName(v bool) string {
	if v {
		return "interactive"
	}
	return "headless"
}

func TestWaitUntilMutatingDenied(t *testing.T) {
	t.Parallel()
	tool := newWaitUntilTool(t.TempDir())
	_, err := tool.Execute(testContext(t), map[string]any{
		tools.WaitUntilParamCommand: "systemctl restart nginx",
	})
	var te *tools.ToolError
	if !errors.As(err, &te) || te.Code != tools.ErrCodeInvalidArgs {
		t.Fatalf("err = %v, want INVALID_ARGS", err)
	}
}

func TestWaitUntilSubagentNotRegistered(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test",
		WorkDir:     t.TempDir(),
		Interactive: false,
		AgentID:     "child-1",
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if _, ok := runner.Registry().Lookup(tools.WaitUntilToolName); ok {
		t.Fatal("wait_until must not register on a subagent")
	}
}

func TestWaitUntilRegisteredOnParent(t *testing.T) {
	t.Parallel()
	runner, err := NewRunner(RunnerConfig{
		Generator:   &fakeGenerator{},
		Model:       "test",
		WorkDir:     t.TempDir(),
		Interactive: false,
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	if _, ok := runner.Registry().Lookup(tools.WaitUntilToolName); !ok {
		t.Fatal("wait_until missing on parent registry")
	}
}
