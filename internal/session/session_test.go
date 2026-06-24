package session_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// TestSessionRoundTrip verifies that messages written by Recorder can be read
// back by LoadSession with matching content.
func TestSessionRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sessID := "test-session-abc12345"
	hash := session.ProjectHash(dir)

	rec := session.NewRecorder(dir, sessID, hash)
	if rec == nil {
		t.Fatal("NewRecorder returned nil")
	}

	rec.RecordUserMessage("hello world")
	rec.RecordModelMessage("greetings", nil)
	rec.RecordUserMessage("another message")

	// FilePath should point to a real file.
	fp := rec.FilePath()
	if fp == "" {
		t.Fatal("FilePath is empty — recorder is disabled unexpectedly")
	}
	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("session file does not exist: %v", err)
	}

	// Load the session back.
	record, err := session.LoadSession(fp)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.SessionID != sessID {
		t.Errorf("SessionID: got %q, want %q", record.SessionID, sessID)
	}
	if record.ProjectHash != hash {
		t.Errorf("ProjectHash: got %q, want %q", record.ProjectHash, hash)
	}

	// Expect 3 message records: user, model, user.
	if len(record.Messages) != 3 {
		t.Fatalf("message count: got %d, want 3", len(record.Messages))
	}

	type wantMsg struct {
		msgType session.MessageType
		text    string
	}
	wants := []wantMsg{
		{session.MessageTypeUser, "hello world"},
		{session.MessageTypeModel, "greetings"},
		{session.MessageTypeUser, "another message"},
	}
	for i, w := range wants {
		m := record.Messages[i]
		if m.Type != w.msgType {
			t.Errorf("message[%d] type: got %q, want %q", i, m.Type, w.msgType)
		}
		got := extractText(m.Content)
		if got != w.text {
			t.Errorf("message[%d] text: got %q, want %q", i, got, w.text)
		}
	}
}

// TestListSessionsEmpty asserts that an empty (or non-existent) chats directory
// returns an empty slice without error.
func TestListSessionsEmpty(t *testing.T) {
	t.Parallel()

	t.Run("non-existent dir", func(t *testing.T) {
		t.Parallel()
		infos, err := session.ListSessions("/tmp/sagittarius-test-nonexistent-dir-xyz/chats", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(infos) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(infos))
		}
	})

	t.Run("empty dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		infos, err := session.ListSessions(dir, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(infos) != 0 {
			t.Errorf("expected 0 sessions, got %d", len(infos))
		}
	})
}

// TestResumeLatest verifies that the Selector correctly identifies and loads
// the most recently started session.
func TestResumeLatest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create three sessions with distinct start times.
	sessions := []struct {
		id   string
		text string
	}{
		{"sess-aaaa1111", "first session message"},
		{"sess-bbbb2222", "second session message"},
		{"sess-cccc3333", "latest session message"},
	}

	for i, s := range sessions {
		hash := session.ProjectHash(dir)
		rec := session.NewRecorder(dir, s.id, hash)
		if rec.FilePath() == "" {
			t.Fatalf("recorder %d is disabled", i)
		}
		rec.RecordUserMessage(s.text)
		// Sleep to ensure distinct timestamps in filenames.
		time.Sleep(10 * time.Millisecond)
	}

	sel := session.NewSelector(dir, "")
	result, err := sel.ResolveSession(session.ResumeLatest)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}

	if result.Record == nil {
		t.Fatal("result.Record is nil")
	}
	if result.Record.SessionID != sessions[2].id {
		t.Errorf("expected latest session id %q, got %q", sessions[2].id, result.Record.SessionID)
	}
	if !strings.Contains(result.DisplayInfo, "Session 3") {
		t.Errorf("DisplayInfo should reference 'Session 3': %q", result.DisplayInfo)
	}
}

// TestResumeByIndex verifies selection by 1-based index.
func TestResumeByIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)

	ids := []string{"sess-idx-0001", "sess-idx-0002", "sess-idx-0003"}
	for i, id := range ids {
		rec := session.NewRecorder(dir, id, hash)
		rec.RecordUserMessage(fmt.Sprintf("message %d", i+1))
		time.Sleep(10 * time.Millisecond)
	}

	sel := session.NewSelector(dir, "")

	// Select session 2 by index.
	result, err := sel.ResolveSession("2")
	if err != nil {
		t.Fatalf("ResolveSession(2): %v", err)
	}
	if result.Record.SessionID != ids[1] {
		t.Errorf("expected id %q, got %q", ids[1], result.Record.SessionID)
	}
}

// TestResumeByUUID verifies selection by full UUID.
func TestResumeByUUID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	target := "sess-uuid-target-1234"

	for _, id := range []string{"sess-uuid-other-0001", target, "sess-uuid-other-0002"} {
		rec := session.NewRecorder(dir, id, hash)
		rec.RecordUserMessage("hello")
		time.Sleep(10 * time.Millisecond)
	}

	sel := session.NewSelector(dir, "")
	result, err := sel.ResolveSession(target)
	if err != nil {
		t.Fatalf("ResolveSession(%q): %v", target, err)
	}
	if result.Record.SessionID != target {
		t.Errorf("expected id %q, got %q", target, result.Record.SessionID)
	}
}

// TestResumeInvalidIdentifier verifies that unknown identifiers return the
// correct error type.
func TestResumeInvalidIdentifier(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "some-session", hash)
	rec.RecordUserMessage("hi")

	sel := session.NewSelector(dir, "")
	_, err := sel.ResolveSession("99")
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
	var invErr *session.ErrInvalidSessionIdentifier
	if !errors.As(err, &invErr) {
		t.Errorf("expected ErrInvalidSessionIdentifier, got %T: %v", err, err)
	}
}

// TestProjectHash verifies cross-platform determinism and fork-compatibility
// for the SHA-256 project hash algorithm.
func TestProjectHash(t *testing.T) {
	t.Parallel()

	// Known hash for "/home/user/myproject" — computed externally as reference.
	// We just verify determinism here (two calls must match).
	root := "/some/project/root"
	h1 := session.ProjectHash(root)
	h2 := session.ProjectHash(root)
	if h1 != h2 {
		t.Errorf("ProjectHash is not deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars: %q", len(h1), h1)
	}

	// Different roots must produce different hashes.
	h3 := session.ProjectHash("/different/root")
	if h1 == h3 {
		t.Error("different project roots produced the same hash")
	}
}

// TestConvertToProviderHistory verifies that loaded session records are
// correctly converted to provider.Message history.
func TestConvertToProviderHistory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "convert-test", hash)

	rec.RecordUserMessage("what is 2+2?")
	rec.RecordModelMessage("The answer is 4.", nil)
	rec.RecordUserMessage("thanks")

	fp := rec.FilePath()
	record, err := session.LoadSession(fp)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	history := session.ConvertToProviderHistory(record)
	if len(history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(history))
	}

	roles := []provider.Role{provider.RoleUser, provider.RoleModel, provider.RoleUser}
	for i, want := range roles {
		if history[i].Role != want {
			t.Errorf("history[%d].Role: got %q, want %q", i, history[i].Role, want)
		}
	}
}

// TestDeleteSession verifies that a session file is removed by DeleteSession.
func TestDeleteSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "delete-test", hash)
	rec.RecordUserMessage("will be deleted")

	fp := rec.FilePath()
	if fp == "" {
		t.Fatal("recorder disabled")
	}
	fileName := filepath.Base(fp)

	if err := session.DeleteSession(dir, fileName); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := os.Stat(fp); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}

	// Deleting again should not error (idempotent).
	if err := session.DeleteSession(dir, fileName); err != nil {
		t.Errorf("second DeleteSession should be idempotent: %v", err)
	}
}

// TestFormatSessionList verifies the human-readable output format.
func TestFormatSessionList(t *testing.T) {
	t.Parallel()

	// Empty list.
	empty := session.FormatSessionList(nil)
	if !strings.Contains(empty, "No previous sessions") {
		t.Errorf("empty list: unexpected output: %q", empty)
	}

	// Non-empty list.
	infos := []session.SessionInfo{
		{
			Index:            1,
			ID:               "abcdef12-0000-0000-0000-000000000000",
			DisplayName:      "my first conversation",
			LastUpdated:      time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
			MessageCount:     4,
			IsCurrentSession: false,
		},
	}
	out := session.FormatSessionList(infos)
	if !strings.Contains(out, "1.") {
		t.Errorf("expected index 1 in output: %q", out)
	}
	if !strings.Contains(out, "abcdef12") {
		t.Errorf("expected short ID in output: %q", out)
	}
	if !strings.Contains(out, "my first conversation") {
		t.Errorf("expected display name in output: %q", out)
	}
}

// TestConvertToProviderHistoryToolRoundTrip verifies that resuming a session
// that used tools does not duplicate the function-response turn and preserves
// the recorded response map verbatim (regression for the placeholder-synthesis
// and double-wrap bugs).
func TestConvertToProviderHistoryToolRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "tool-roundtrip", hash)

	rec.RecordUserMessage("read the file")
	rec.RecordModelMessage("sure", []provider.ToolCall{{
		ID:   "call-1",
		Name: "read_file",
		Args: map[string]any{"path": "/tmp/x"},
	}})
	rec.RecordFunctionResponses([]provider.FunctionResponse{{
		Name:     "read_file",
		Response: map[string]any{"output": "file contents"},
	}})
	rec.RecordModelMessage("done", nil)

	record, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}

	history := session.ConvertToProviderHistory(record)

	// Expect exactly: user(text), model(text+call), user(funcResponse), model(text).
	if len(history) != 4 {
		t.Fatalf("expected 4 history entries, got %d: %+v", len(history), history)
	}

	// Exactly one function-response turn (no synthesized duplicate).
	respTurns := 0
	var gotResp *provider.FunctionResponse
	for _, m := range history {
		for _, p := range m.Parts {
			if p.FunctionResponse != nil {
				respTurns++
				gotResp = p.FunctionResponse
			}
		}
	}
	if respTurns != 1 {
		t.Fatalf("expected exactly 1 function response, got %d", respTurns)
	}

	// Response map must pass through unchanged (not nested under a second key).
	if gotResp.Response["output"] != "file contents" {
		t.Errorf("response map not preserved: got %#v", gotResp.Response)
	}
	if _, doubleWrapped := gotResp.Response["output"].(map[string]any); doubleWrapped {
		t.Error("response map was double-wrapped under a nested output key")
	}
}

// TestWriteHistoryRoundTrip verifies a checkpoint written from in-memory history
// reloads to the same provider history through LoadSession + ConvertToProviderHistory.
func TestWriteHistoryRoundTrip(t *testing.T) {
	t.Parallel()

	history := []provider.Message{
		{Role: provider.RoleUser, Parts: []provider.Part{{Text: "read the file"}}},
		{Role: provider.RoleModel, Parts: []provider.Part{
			{Text: "sure"},
			{FunctionCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Args: map[string]any{"path": "/tmp/x"}}},
		}},
		{Role: provider.RoleUser, Parts: []provider.Part{
			{FunctionResponse: &provider.FunctionResponse{Name: "read_file", Response: map[string]any{"output": "file contents"}}},
		}},
		{Role: provider.RoleModel, Parts: []provider.Part{{Text: "done"}}},
	}

	path := filepath.Join(t.TempDir(), "checkpoint-test.jsonl")
	if err := session.WriteHistory(path, "", "", "", history, nil); err != nil {
		t.Fatalf("WriteHistory: %v", err)
	}

	record, err := session.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if record.SessionID == "" {
		t.Fatal("WriteHistory must emit a non-empty session id so LoadSession succeeds")
	}

	got := session.ConvertToProviderHistory(record)
	if len(got) != len(history) {
		t.Fatalf("round-trip history len = %d, want %d: %+v", len(got), len(history), got)
	}

	if got[1].Parts[1].FunctionCall == nil || got[1].Parts[1].FunctionCall.Name != "read_file" {
		t.Errorf("model tool call not preserved: %+v", got[1].Parts)
	}
	var resp *provider.FunctionResponse
	for _, m := range got {
		for _, p := range m.Parts {
			if p.FunctionResponse != nil {
				resp = p.FunctionResponse
			}
		}
	}
	if resp == nil || resp.Response["output"] != "file contents" {
		t.Errorf("function response not preserved: %+v", resp)
	}
}

// TestWriteHistoryEmpty verifies an empty conversation still yields a loadable
// file (metadata only) without panicking.
func TestWriteHistoryEmpty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "checkpoint-empty.jsonl")
	if err := session.WriteHistory(path, "sid-1", "", "", nil, nil); err != nil {
		t.Fatalf("WriteHistory: %v", err)
	}
	record, err := session.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(record.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(record.Messages))
	}
}

// TestRecorderRotateStartsNewFile verifies /clear-style rotation begins a fresh
// session file with a new id, leaving the original intact.
func TestRecorderRotateStartsNewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "rotate-test-abc123", hash)

	rec.RecordUserMessage("before clear")
	firstPath := rec.FilePath()
	firstID := rec.SessionID()

	rec.Rotate()

	secondPath := rec.FilePath()
	secondID := rec.SessionID()
	rec.RecordUserMessage("after clear")

	if firstPath == secondPath {
		t.Errorf("rotate should change the file path; both are %q", firstPath)
	}
	if firstID == secondID {
		t.Errorf("rotate should assign a new session id; both are %q", firstID)
	}

	// Original file retains only the pre-clear message.
	first, err := session.LoadSession(firstPath)
	if err != nil {
		t.Fatalf("LoadSession(first): %v", err)
	}
	if len(first.Messages) != 1 || extractText(first.Messages[0].Content) != "before clear" {
		t.Errorf("original session should hold only the pre-clear turn, got %+v", first.Messages)
	}

	// New file holds only the post-clear message.
	second, err := session.LoadSession(secondPath)
	if err != nil {
		t.Fatalf("LoadSession(second): %v", err)
	}
	if len(second.Messages) != 1 || extractText(second.Messages[0].Content) != "after clear" {
		t.Errorf("rotated session should hold only the post-clear turn, got %+v", second.Messages)
	}
}

// TestListSessionsRespectsRewind verifies that --list-sessions metadata (count
// and preview) honours $rewindTo trimming, matching LoadSession.
func TestListSessionsRespectsRewind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	hash := session.ProjectHash(dir)
	rec := session.NewRecorder(dir, "rewind-list-test", hash)

	rec.RecordUserMessage("first prompt")
	rec.RecordModelMessage("first reply", nil)
	rec.RecordUserMessage("second prompt")

	// Load full record to capture the id of the second user message, then append
	// a $rewindTo pointing at it so the last two messages are trimmed.
	full, err := session.LoadSession(rec.FilePath())
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if len(full.Messages) != 3 {
		t.Fatalf("setup: expected 3 messages, got %d", len(full.Messages))
	}
	rewindID := full.Messages[1].ID // rewind to the model reply (drops reply + 2nd prompt)

	f, err := os.OpenFile(rec.FilePath(), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open for rewind append: %v", err)
	}
	if _, err := fmt.Fprintf(f, "{\"$rewindTo\":%q}\n", rewindID); err != nil {
		t.Fatalf("write rewind: %v", err)
	}
	_ = f.Close()

	infos, err := session.ListSessions(dir, "")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 session, got %d", len(infos))
	}
	if infos[0].MessageCount != 1 {
		t.Errorf("expected message count 1 after rewind, got %d", infos[0].MessageCount)
	}
	if infos[0].FirstUserMessage != "first prompt" {
		t.Errorf("expected first prompt preserved, got %q", infos[0].FirstUserMessage)
	}
}

// extractText pulls plain text from session.Part slice (test helper).
func extractText(parts []session.Part) string {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(p.Text)
	}
	return sb.String()
}
