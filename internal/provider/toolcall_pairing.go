package provider

import "slices"

// toolCallPairer assigns wire IDs to tool results whose CallID was lost, such
// as results rebuilt by older context masking or loaded from sessions saved
// before CallIDs were recorded. Strict endpoints (Anthropic, OpenAI) require
// each result to reference a call in the immediately preceding assistant turn,
// so a missing ID may only be filled from that turn's unanswered calls with the
// same tool name. Results that carry an ID consume their call too; otherwise
// an ID-less result would bind to a call that was already answered.
type toolCallPairer struct {
	pending      map[string][]string
	afterResults bool
}

func newToolCallPairer() *toolCallPairer {
	return &toolCallPairer{pending: make(map[string][]string)}
}

// addCall records an assistant tool call. The first call after any result
// starts a new assistant turn, so calls from earlier turns stop being
// candidates.
func (p *toolCallPairer) addCall(name, id string) {
	if p.afterResults {
		clear(p.pending)
		p.afterResults = false
	}
	p.pending[name] = append(p.pending[name], id)
}

// resolve returns the wire ID for a tool result. A non-empty id is returned
// unchanged and removed from the pending set; an empty id takes the oldest
// unanswered call with the same name, or fallback when none remains.
func (p *toolCallPairer) resolve(name, id, fallback string) string {
	p.afterResults = true
	queue := p.pending[name]
	if id != "" {
		if i := slices.Index(queue, id); i >= 0 {
			p.pending[name] = slices.Delete(queue, i, i+1)
		}
		return id
	}
	if len(queue) == 0 {
		return fallback
	}
	p.pending[name] = queue[1:]
	return queue[0]
}
