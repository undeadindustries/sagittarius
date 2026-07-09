package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/undeadindustries/sagittarius/internal/goal"
	"github.com/undeadindustries/sagittarius/internal/grill"
	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Recorder streams conversation turns to a JSONL session file.
// It is safe for concurrent access from the agent loop and slash command handlers.
type Recorder struct {
	mu          sync.Mutex
	chatsDir    string
	filePath    string
	sessionID   string
	projectHash string
	disabled    bool // set on ENOSPC or init failure
}

// NewRecorder creates a new session file under chatsDir and writes the initial
// metadata line. Returns a disabled (no-op) recorder on error so the caller
// can always call Record* methods without nil checks.
func NewRecorder(chatsDir, sessionID, projectHash string) *Recorder {
	r := &Recorder{
		chatsDir:    chatsDir,
		sessionID:   sessionID,
		projectHash: projectHash,
	}

	if err := os.MkdirAll(chatsDir, 0o700); err != nil {
		slog.Warn("session: cannot create chats dir, recording disabled", "err", err)
		r.disabled = true
		return r
	}

	ts := time.Now().UTC().Format("2006-01-02T15-04")
	// fileKey is the first 8 chars of the session ID when it is UUID-like
	// (random), matching the fork's filename format. For non-UUID session IDs
	// (e.g. "sagittarius-<pid>"), we derive an 8-char random hex suffix to
	// prevent filename collisions when multiple sessions start in the same minute.
	fileKey := deriveFileKey(sessionID)
	filename := fmt.Sprintf("%s%s-%s.jsonl", SessionFilePrefix, ts, fileKey)
	r.filePath = filepath.Join(chatsDir, filename)

	meta := MetadataRecord{
		SessionID:   sessionID,
		ProjectHash: projectHash,
		StartTime:   time.Now().UTC().Format(time.RFC3339Nano),
		LastUpdated: time.Now().UTC().Format(time.RFC3339Nano),
		Kind:        "main",
	}
	if err := r.appendLine(meta); err != nil {
		slog.Warn("session: cannot write initial metadata, recording disabled", "err", err)
		r.disabled = true
	}
	return r
}

// SessionID returns the session identifier being recorded. Guarded by r.mu
// because Rotate() writes r.sessionID concurrently (e.g. /clear or /chat resume
// rotating the recorder while the agent loop reads the id).
func (r *Recorder) SessionID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessionID
}

// FilePath returns the JSONL file path, or "" if recording is disabled.
func (r *Recorder) FilePath() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.filePath
}

// Rotate abandons the current session file and begins a new one with a fresh
// session id in the same chats directory, writing a new metadata line. It backs
// the /clear command so subsequent turns are recorded to a new session rather
// than appended to the cleared conversation. On any filesystem error the
// recorder becomes disabled (a no-op) instead of writing to the old file.
func (r *Recorder) Rotate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.chatsDir == "" {
		r.disabled = true
		return
	}

	newSessionID := newID()
	ts := time.Now().UTC().Format("2006-01-02T15-04")
	filename := fmt.Sprintf("%s%s-%s.jsonl", SessionFilePrefix, ts, deriveFileKey(newSessionID))
	r.filePath = filepath.Join(r.chatsDir, filename)
	r.sessionID = newSessionID
	r.disabled = false

	meta := MetadataRecord{
		SessionID:   newSessionID,
		ProjectHash: r.projectHash,
		StartTime:   time.Now().UTC().Format(time.RFC3339Nano),
		LastUpdated: time.Now().UTC().Format(time.RFC3339Nano),
		Kind:        "main",
	}
	if err := r.appendLineLocked(meta); err != nil {
		slog.Warn("session: cannot write metadata after rotate, recording disabled", "err", err)
		r.disabled = true
	}
}

// RecordUserMessage appends a user message to the session file.
func (r *Recorder) RecordUserMessage(text string) {
	r.record(MessageRecord{
		ID:        newID(),
		Timestamp: now(),
		Type:      MessageTypeUser,
		Content:   textParts(text),
	})
}

// RecordModelMessage appends a model (assistant) message.
func (r *Recorder) RecordModelMessage(text string, calls []provider.ToolCall) {
	parts := textParts(text)
	toolRecords := make([]ToolCallRecord, 0, len(calls))
	for _, c := range calls {
		parts = append(parts, Part{FunctionCall: &FunctionCallPart{
			ID:   c.ID,
			Name: c.Name,
			Args: c.Args,
		}})
		toolRecords = append(toolRecords, ToolCallRecord{
			ID:     c.ID,
			Name:   c.Name,
			Status: "success",
		})
	}
	r.record(MessageRecord{
		ID:        newID(),
		Timestamp: now(),
		Type:      MessageTypeModel,
		Content:   parts,
		ToolCalls: toolRecords,
	})
	r.updateLastUpdated()
}

// RecordFunctionResponses appends tool responses as a user turn.
func (r *Recorder) RecordFunctionResponses(responses []provider.FunctionResponse) {
	if len(responses) == 0 {
		return
	}
	parts := make([]Part, 0, len(responses))
	for _, resp := range responses {
		parts = append(parts, Part{FunctionResponse: &FuncResponsePart{
			ID:       resp.CallID,
			Name:     resp.Name,
			Response: resp.Response,
		}})
	}
	r.record(MessageRecord{
		ID:        newID(),
		Timestamp: now(),
		Type:      MessageTypeUser,
		Content:   parts,
	})
}

// record appends a message record and updates lastUpdated in the file.
func (r *Recorder) record(msg MessageRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	if err := r.appendLineLocked(msg); err != nil {
		r.handleWriteError(err)
	}
}

// updateLastUpdated appends a $set record to refresh the lastUpdated timestamp.
func (r *Recorder) updateLastUpdated() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	update := map[string]interface{}{
		"$set": map[string]string{
			"lastUpdated": now(),
		},
	}
	if err := r.appendLineLocked(update); err != nil {
		r.handleWriteError(err)
	}
}

func (r *Recorder) appendLine(v interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.appendLineLocked(v)
}

func (r *Recorder) appendLineLocked(v interface{}) error {
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("session: marshal record: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(r.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("session: open file: %w", err)
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(line)
	return err
}

// RecordSessionGrant appends a $set record adding a tool to the session grants list.
func (r *Recorder) RecordSessionGrant(toolName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return nil
	}
	set := SetRecord{
		Set: &MetadataRecord{
			SessionGrants: []string{toolName},
		},
	}
	return r.appendLineLocked(set)
}

// SetGoal appends a goal metadata update to the session file.
func (r *Recorder) SetGoal(goal *goal.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return nil
	}
	set := SetRecord{
		Set: &MetadataRecord{
			Goal: goal,
		},
	}
	return r.appendLineLocked(set)
}

// SetGrill appends a grill-me session metadata update to the session file.
func (r *Recorder) SetGrill(g *grill.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return nil
	}
	set := SetRecord{
		Set: &MetadataRecord{
			Grill: g,
		},
	}
	return r.appendLineLocked(set)
}

func (r *Recorder) handleWriteError(err error) {
	if isENOSPC(err) {
		r.disabled = true
		slog.Warn("session recording disabled: no space left on device")
		return
	}
	slog.Error("session: write error", "err", err)
}

func isENOSPC(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no space left on device")
}

func textParts(text string) []Part {
	if text == "" {
		return []Part{}
	}
	return []Part{{Text: text}}
}

// deriveFileKey returns an 8-character string suitable for use in a session
// filename. For UUID-like session IDs (32+ hex chars, possibly with dashes)
// it returns the first 8 significant chars. Otherwise it generates 4 random
// bytes as a hex string to avoid collisions.
func deriveFileKey(sessionID string) string {
	stripped := strings.ReplaceAll(sessionID, "-", "")
	// Only use the sessionID directly if it looks like a UUID (hex chars).
	if len(stripped) >= 8 && isHexString(stripped) {
		return stripped[:8]
	}
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b[:])
}

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// newID generates a random UUID v4 using crypto/rand.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Extremely unlikely; fall back to timestamp-based id.
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
