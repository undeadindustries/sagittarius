package tools

import (
	"context"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

func TestConfirmGateZeroValuePrompts(t *testing.T) {
	t.Parallel()
	var gate ConfirmGate
	if gate.AutoApprove() || gate.Granted("wait_until") {
		t.Fatal("zero gate must not auto-approve or report a grant")
	}
	gate.Grant("wait_until")
	if ConfirmGateFrom(context.Background()).AutoApprove() {
		t.Fatal("a context with no gate must yield the zero gate")
	}
}

const confirmGateProbeName = "confirm_gate_probe"

type confirmGateProbe struct {
	autoApprove bool
	granted     bool
	doGrant     bool
}

func (p *confirmGateProbe) Name() string { return confirmGateProbeName }
func (p *confirmGateProbe) Declaration() provider.ToolDeclaration {
	return provider.ToolDeclaration{Name: confirmGateProbeName}
}
func (p *confirmGateProbe) RequiresConfirmation() bool { return false }
func (p *confirmGateProbe) Execute(context.Context, map[string]any) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}
func (p *confirmGateProbe) ExecuteInteractive(ctx context.Context, _ map[string]any, _ bool, _ func(ui.StreamEvent)) (map[string]any, error) {
	gate := ConfirmGateFrom(ctx)
	p.autoApprove = gate.AutoApprove()
	p.granted = gate.Granted(confirmGateProbeName)
	if p.doGrant {
		gate.Grant(confirmGateProbeName)
	}
	return map[string]any{"ok": true}, nil
}

func TestSchedulerConfirmGateYoloAndGrant(t *testing.T) {
	t.Parallel()

	t.Run("yolo", func(t *testing.T) {
		t.Parallel()
		probe := &confirmGateProbe{}
		s := confirmGateScheduler(probe, Policy{Mode: ApprovalYolo}, nil)
		if _, err := s.Execute(context.Background(), []provider.ToolCall{{
			ID: "c1", Name: confirmGateProbeName,
		}}, func(ui.StreamEvent) {}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !probe.autoApprove {
			t.Fatal("yolo gate must auto-approve")
		}
	})

	t.Run("grant", func(t *testing.T) {
		t.Parallel()
		probe := &confirmGateProbe{doGrant: true}
		var recorded []string
		s := confirmGateScheduler(probe, Policy{Mode: ApprovalDefault}, func(name string) {
			recorded = append(recorded, name)
		})
		calls := []provider.ToolCall{{ID: "c1", Name: confirmGateProbeName}}
		if _, err := s.Execute(context.Background(), calls, func(ui.StreamEvent) {}); err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if probe.autoApprove {
			t.Fatal("default mode must not auto-approve")
		}
		if !containsGrant(s.SessionGrants(), confirmGateProbeName) {
			t.Fatalf("SessionGrants = %v, want %s", s.SessionGrants(), confirmGateProbeName)
		}
		if len(recorded) != 1 || recorded[0] != confirmGateProbeName {
			t.Fatalf("recorder = %v, want [%s]", recorded, confirmGateProbeName)
		}

		again := &confirmGateProbe{}
		s.registry.Register(again)
		if _, err := s.Execute(context.Background(), calls, func(ui.StreamEvent) {}); err != nil {
			t.Fatalf("second Execute: %v", err)
		}
		if !again.granted {
			t.Fatal("a later call must see the session grant")
		}
		if len(recorded) != 1 {
			t.Fatalf("recorder fired %d times, want 1", len(recorded))
		}
	})
}

func confirmGateScheduler(probe *confirmGateProbe, policy Policy, record func(string)) *Scheduler {
	registry := &Registry{
		byName:  map[string]Tool{confirmGateProbeName: probe},
		aliases: map[string]string{},
	}
	return NewScheduler(registry, policy, true, nil, nil, WithSessionGrantRecorder(record))
}

func containsGrant(grants []string, name string) bool {
	for _, g := range grants {
		if g == name {
			return true
		}
	}
	return false
}
