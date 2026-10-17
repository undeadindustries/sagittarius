package tools

import "context"

// ConfirmGate carries the scheduler's approval policy into a tool that prompts
// from inside Execute. The scheduler never prompts for a tool whose
// RequiresConfirmation is false, so those tools would otherwise ignore yolo
// and session grants. A zero ConfirmGate prompts: that is what ConfirmGateFrom
// returns when the scheduler did not attach one, and what unit tests get when
// they call a tool directly.
type ConfirmGate struct {
	autoApprove bool
	granted     func(string) bool
	grant       func(string)
}

type confirmGateContextKey struct{}

// NewConfirmGate builds a gate. granted and grant may be nil. AutoApprove,
// Granted, and Grant are safe on the zero value.
func NewConfirmGate(autoApprove bool, granted func(string) bool, grant func(string)) ConfirmGate {
	return ConfirmGate{autoApprove: autoApprove, granted: granted, grant: grant}
}

// AutoApprove reports whether the session's approval mode skips confirmation.
// Yolo is the only mode that does; autoEdit still confirms an unclassified
// command because that command is not an edit.
func (g ConfirmGate) AutoApprove() bool { return g.autoApprove }

// Granted reports whether name was approved for this session. A nil lookup
// reports false.
func (g ConfirmGate) Granted(name string) bool {
	if g.granted == nil || name == "" {
		return false
	}
	return g.granted(name)
}

// Grant records a session-wide approval for name. A nil recorder is a no-op,
// so a tool running outside the scheduler can still call it.
func (g ConfirmGate) Grant(name string) {
	if g.grant == nil || name == "" {
		return
	}
	g.grant(name)
}

// WithConfirmGate returns a context carrying gate. A nil context is returned
// unchanged.
func WithConfirmGate(ctx context.Context, gate ConfirmGate) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, confirmGateContextKey{}, gate)
}

// ConfirmGateFrom returns the gate stored by WithConfirmGate, or the zero
// value when ctx is nil or has none.
func ConfirmGateFrom(ctx context.Context) ConfirmGate {
	if ctx == nil {
		return ConfirmGate{}
	}
	gate, _ := ctx.Value(confirmGateContextKey{}).(ConfirmGate)
	return gate
}
