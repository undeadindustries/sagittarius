package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// verboseLog writes a human-readable, timestamped transcript of everything
// sent to and received from the model — full wire requests, assembled model
// output, and tool results — to a caller-supplied writer. It backs
// --log-verbose, a rarely-used opt-in flag whose only purpose is producing a
// complete record a user can attach to a bug report; it is independent of
// --debug (which only raises slog's level for operational log lines).
//
// Every method is nil-safe (a no-op on a nil *verboseLog) so call sites in the
// hot turn loop never need to branch on whether verbose logging is enabled.
type verboseLog struct {
	mu  sync.Mutex
	out io.WriteCloser
}

// newVerboseLog wraps w (already open for append) in a verboseLog. A nil w
// yields a nil *verboseLog, which remains safe to call methods on.
func newVerboseLog(w io.WriteCloser) *verboseLog {
	if w == nil {
		return nil
	}
	return &verboseLog{out: w}
}

// LogTurnStart records the raw text the user submitted for this turn, before
// "@path" expansion (the expanded content shows up verbatim in the next
// LogRequest, so it is not duplicated here).
func (v *verboseLog) LogTurnStart(userInput string) {
	if v == nil {
		return
	}
	v.writeSection("IN  user message", userInput)
}

// LogRequest records the request body sent to the provider for one round of
// the tool loop, preferring the exact wire JSON when the generator can
// produce it (see Runner.debugRequestBody).
func (v *verboseLog) LogRequest(round int, body []byte, err error) {
	if v == nil {
		return
	}
	if err != nil {
		v.writeSection(fmt.Sprintf("OUT request (round %d)", round), "(failed to serialize request: "+err.Error()+")")
		return
	}
	v.writeSection(fmt.Sprintf("OUT request (round %d)", round), string(body))
}

// LogResponse records the assembled model output for one round: answer text,
// tool calls the model asked to run, and provider-reported usage.
func (v *verboseLog) LogResponse(round int, text string, toolCalls []provider.ToolCall, usage *provider.Usage) {
	if v == nil {
		return
	}
	var b strings.Builder
	if text != "" {
		fmt.Fprintf(&b, "text:\n%s\n", text)
	}
	for _, call := range toolCalls {
		fmt.Fprintf(&b, "tool_call: %s(%s) id=%s\n", call.Name, jsonCompact(call.Args), call.ID)
	}
	if usage != nil {
		fmt.Fprintf(&b, "usage: in=%d out=%d cost_known=%v cost_usd=%.4f\n",
			usage.InputTokens, usage.OutputTokens, usage.CostKnown, usage.CostUSD)
	}
	if b.Len() == 0 {
		b.WriteString("(no text, no tool calls)")
	}
	v.writeSection(fmt.Sprintf("IN  model response (round %d)", round), b.String())
}

// LogToolResult records one tool's result (or error payload) after execution.
func (v *verboseLog) LogToolResult(resp provider.FunctionResponse) {
	if v == nil {
		return
	}
	v.writeSection("OUT tool_result "+resp.Name, jsonCompact(resp.Response))
}

// LogError records a terminal stream error surfaced to the user for this turn.
func (v *verboseLog) LogError(err error) {
	if v == nil || err == nil {
		return
	}
	v.writeSection("ERROR", err.Error())
}

// LogInfo records a one-line informational note (e.g. "max tool rounds
// exceeded") that ended a turn without a Go error value.
func (v *verboseLog) LogInfo(text string) {
	if v == nil || text == "" {
		return
	}
	v.writeSection("INFO", text)
}

func (v *verboseLog) writeSection(kind, body string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	fmt.Fprintf(v.out, "\n===== %s | %s =====\n%s\n", time.Now().Format(time.RFC3339), kind, strings.TrimRight(body, "\n"))
}

// Close closes the underlying writer. Safe to call on a nil *verboseLog.
func (v *verboseLog) Close() error {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.out.Close()
}

// jsonCompact renders v as compact JSON for a log line, falling back to a Go
// %v representation if marshaling fails (never returns an error to the caller
// since this only ever feeds a best-effort log line).
func jsonCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}
