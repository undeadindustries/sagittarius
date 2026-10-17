package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"sync"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// requestFingerprint captures the deterministic prefix hashes of one outbound
// GenerateRequest so the next round can verify prefix stability.
type requestFingerprint struct {
	SystemHash string
	ToolsHash  string
	MsgHashes  []uint64
}

// prefixObservation describes the prefix-stability check for one outbound round.
type prefixObservation struct {
	Round      int
	SystemHash string
	ToolsHash  string
	Broken     bool
	BreakIndex int    // -1 when Broken is false; 0 for system/tools; message index for history
	Reason     string // e.g. "compression", "masking", "mode-change", "unexpected:system"
	Unexpected bool
}

// prefixDetector tracks request fingerprints across rounds and classifies
// prefix breaks as expected (caused by a known runner operation) or unexpected.
type prefixDetector struct {
	mu             sync.Mutex
	hasPrev        bool
	prev           requestFingerprint
	round          int
	expectedReason string
}

// noteExpectedBreak records that an intentional operation (compression, masking,
// truncation, mode switch, etc.) just mutated the request prefix. If multiple
// operations occur before the next round, the first recorded reason is kept
// unless superseded by "compression".
func (d *prefixDetector) noteExpectedBreak(reason string) {
	if reason == "" {
		return
	}
	d.mu.Lock()
	if d.expectedReason == "" || reason == "compression" {
		d.expectedReason = reason
	}
	d.mu.Unlock()
}

// reset clears the detector state when history is wiped or switched.
func (d *prefixDetector) reset() {
	d.mu.Lock()
	d.hasPrev = false
	d.prev = requestFingerprint{}
	d.round = 0
	d.expectedReason = ""
	d.mu.Unlock()
}

// observe fingerprints req, compares it against the previous round's prefix,
// and consumes any pending expected-break reason.
func (d *prefixDetector) observe(req *provider.GenerateRequest) prefixObservation {
	fp := fingerprintRequest(req)

	d.mu.Lock()
	defer d.mu.Unlock()

	d.round++
	expected := d.expectedReason
	d.expectedReason = ""

	obs := prefixObservation{
		Round:      d.round,
		SystemHash: fp.SystemHash,
		ToolsHash:  fp.ToolsHash,
		BreakIndex: -1,
	}
	if !d.hasPrev {
		d.hasPrev = true
		d.prev = fp
		return obs
	}

	broken, pos, comp := diffRequestFingerprints(d.prev, fp)
	d.prev = fp
	if !broken {
		return obs
	}

	obs.Broken = true
	obs.BreakIndex = pos
	if expected != "" {
		obs.Reason = expected
		return obs
	}
	obs.Reason = "unexpected:" + comp
	obs.Unexpected = true
	return obs
}

// fingerprintRequest computes the System (SHA-256), Tools (SHA-256), and
// per-message History (FNV-64a) hashes for req.
func fingerprintRequest(req *provider.GenerateRequest) requestFingerprint {
	if req == nil {
		return requestFingerprint{}
	}
	var fp requestFingerprint
	if req.SystemInstruction != "" {
		sum := sha256.Sum256([]byte(req.SystemInstruction))
		fp.SystemHash = hex.EncodeToString(sum[:])
	}
	if len(req.Tools) > 0 {
		if raw, err := json.Marshal(req.Tools); err == nil {
			sum := sha256.Sum256(raw)
			fp.ToolsHash = hex.EncodeToString(sum[:])
		}
	}
	if len(req.Messages) > 0 {
		fp.MsgHashes = make([]uint64, len(req.Messages))
		for i := range req.Messages {
			fp.MsgHashes[i] = hashMessageWithReasoning(req.Messages[i], req.KeepReasoningHistory)
		}
	}
	return fp
}

// hashMessageWithReasoning computes a deterministic 64-bit FNV-1a hash of one provider.Message.
func hashMessageWithReasoning(m provider.Message, keepReasoning bool) uint64 {
	h := fnv.New64a()
	writeHashStr(h, string(m.Role))
	for i := range m.Parts {
		p := &m.Parts[i]
		writeHashStr(h, p.Text)
		if keepReasoning && p.Reasoning != "" {
			writeHashStr(h, p.Reasoning)
		}
		if len(p.ThoughtSignature) > 0 {
			_, _ = h.Write(p.ThoughtSignature)
		}
		_, _ = h.Write([]byte{0xff})
		if p.FunctionCall != nil {
			writeHashStr(h, p.FunctionCall.ID)
			writeHashStr(h, p.FunctionCall.Name)
			if raw, err := json.Marshal(p.FunctionCall.Args); err == nil {
				_, _ = h.Write(raw)
			}
			_, _ = h.Write([]byte{0xfe})
		}
		if p.FunctionResponse != nil {
			writeHashStr(h, p.FunctionResponse.CallID)
			writeHashStr(h, p.FunctionResponse.Name)
			if raw, err := json.Marshal(p.FunctionResponse.Response); err == nil {
				_, _ = h.Write(raw)
			}
			_, _ = h.Write([]byte{0xfd})
		}
	}
	return h.Sum64()
}

func writeHashStr(h interface{ Write([]byte) (int, error) }, s string) {
	_, _ = h.Write([]byte(s))
	_, _ = h.Write([]byte{0})
}

// diffRequestFingerprints compares prev and curr and reports whether the prefix
// was invalidated, the first differing position (0 for system/tools, message
// index for history), and which component changed ("system", "tools", "history").
func diffRequestFingerprints(prev, curr requestFingerprint) (broken bool, pos int, component string) {
	if curr.SystemHash != prev.SystemHash {
		return true, 0, "system"
	}
	if curr.ToolsHash != prev.ToolsHash {
		return true, 0, "tools"
	}
	limit := len(prev.MsgHashes)
	if len(curr.MsgHashes) < limit {
		limit = len(curr.MsgHashes)
	}
	for i := 0; i < limit; i++ {
		if curr.MsgHashes[i] != prev.MsgHashes[i] {
			return true, i, "history"
		}
	}
	if len(curr.MsgHashes) < len(prev.MsgHashes) {
		return true, len(curr.MsgHashes), "history"
	}
	return false, -1, ""
}
