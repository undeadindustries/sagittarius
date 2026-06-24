package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// Manager ties together a Recorder (write) and a Selector (read) for a single
// interactive or headless session.
type Manager struct {
	chatsDir  string
	recorder  *Recorder
	selector  *Selector
	sessionID string
}

// NewManager resolves the chats directory from projectRoot and starts recording.
// projectRoot must be an absolute path. Pass a pre-generated sessionID so the
// context manager and session manager share the same ID.
func NewManager(projectRoot, sessionID string) (*Manager, error) {
	chatsDir, err := ChatsDir(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("session: resolve chats dir: %w", err)
	}

	hash := ProjectHash(projectRoot)
	rec := NewRecorder(chatsDir, sessionID, hash)

	return &Manager{
		chatsDir:  chatsDir,
		recorder:  rec,
		selector:  NewSelector(chatsDir, sessionID),
		sessionID: sessionID,
	}, nil
}

// NewManagerForResume creates a Manager that resumes an existing session.
// It creates a new Recorder pointing at the existing file, not a brand-new one.
func NewManagerForResume(projectRoot, sessionID string, result *SelectionResult) (*Manager, error) {
	chatsDir, err := ChatsDir(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("session: resolve chats dir: %w", err)
	}

	// For resumed sessions we create a no-op recorder (the file is loaded from
	// disk; we don't want to re-append the initial metadata line).
	rec := &Recorder{
		chatsDir:    chatsDir,
		filePath:    result.SessionPath,
		sessionID:   result.Record.SessionID,
		projectHash: result.Record.ProjectHash,
	}

	return &Manager{
		chatsDir:  chatsDir,
		recorder:  rec,
		selector:  NewSelector(chatsDir, sessionID),
		sessionID: sessionID,
	}, nil
}

// Recorder returns the active write recorder.
func (m *Manager) Recorder() *Recorder {
	return m.recorder
}

// Selector returns the session selector (read side).
func (m *Manager) Selector() *Selector {
	return m.selector
}

// ChatsDir returns the resolved chats directory path.
func (m *Manager) ChatsDir() string {
	return m.chatsDir
}

// SessionID returns the active session ID.
func (m *Manager) SessionID() string {
	return m.sessionID
}

// ConvertToProviderHistory converts a loaded ConversationRecord to provider.Messages
// suitable for injection into an agent Runner's history.
//
// Rules (matching fork convertSessionToClientHistory):
//   - info / error / warning messages are skipped
//   - user messages that start with "/" or "?" are skipped
//   - model messages become provider.RoleModel; their function calls are inlined
//   - function responses are emitted as a following provider.RoleUser message
//
// The recorder already persists real tool results as the user message that
// follows a model turn's function calls (see Recorder.RecordFunctionResponses),
// so this function does not synthesize placeholder outputs from msg.ToolCalls —
// doing so would duplicate the function-response turn on resume.
func ConvertToProviderHistory(record *ConversationRecord) []provider.Message {
	var history []provider.Message

	for _, msg := range record.Messages {
		switch msg.Type {
		case MessageTypeInfo, MessageTypeError, MessageTypeWarning:
			continue

		case MessageTypeUser:
			text := extractTextFromParts(msg.Content)
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "?") {
				continue
			}
			// Rebuild parts from function responses if present.
			parts := buildProviderParts(msg.Content)
			if len(parts) == 0 {
				continue
			}
			history = append(history, provider.Message{
				Role:  provider.RoleUser,
				Parts: parts,
			})

		case MessageTypeModel:
			parts := buildProviderParts(msg.Content)
			if len(parts) == 0 {
				continue
			}
			history = append(history, provider.Message{
				Role:  provider.RoleModel,
				Parts: parts,
			})
		}
	}

	return history
}

func buildProviderParts(parts []Part) []provider.Part {
	out := make([]provider.Part, 0, len(parts))
	for _, p := range parts {
		switch {
		case p.Text != "":
			out = append(out, provider.Part{Text: p.Text})
		case p.FunctionCall != nil:
			tc := provider.ToolCall{
				ID:   p.FunctionCall.ID,
				Name: p.FunctionCall.Name,
				Args: p.FunctionCall.Args,
			}
			out = append(out, provider.Part{FunctionCall: &tc})
		case p.FunctionResponse != nil:
			resp := provider.FunctionResponse{
				CallID:   p.FunctionResponse.ID,
				Name:     p.FunctionResponse.Name,
				Response: coerceResponseMap(p.FunctionResponse.Response),
			}
			out = append(out, provider.Part{FunctionResponse: &resp})
		}
	}
	return out
}

// coerceResponseMap maps a recorded tool response back to the provider's
// map[string]any shape. The recorder stores the runner's original response map
// verbatim, so a decoded JSON object passes through unchanged; non-object values
// (or a nil response) are wrapped under an "output" key so the provider always
// receives a well-formed response map.
func coerceResponseMap(raw any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	return map[string]any{"output": raw}
}

// WriteHistory writes history to path as a session JSONL file: a metadata line
// followed by one record per message, in the exact format LoadSession reads
// back. It backs /chat checkpoints, persisting the live in-memory conversation
// rather than copying the on-disk session file (which can diverge after a
// rotation or context compression). The file is created or truncated with 0o600
// permissions. A blank sessionID is replaced with a fresh id so LoadSession,
// which requires non-empty metadata, always succeeds.
func WriteHistory(path, sessionID, projectHash, summary string, history []provider.Message, grants []string) error {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = newID()
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("session: create %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	meta := MetadataRecord{
		SessionID:   sessionID,
		ProjectHash: projectHash,
		StartTime:   ts,
		LastUpdated: ts,
		Summary:     summary,
		Kind:        "main",
		SessionGrants: grants,
	}
	if err := writeJSONLine(w, meta); err != nil {
		return err
	}
	for _, msg := range history {
		if err := writeJSONLine(w, messageToRecord(msg)); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("session: write %s: %w", path, err)
	}
	return nil
}

// writeJSONLine marshals v and writes it as a single newline-terminated line.
func writeJSONLine(w *bufio.Writer, v any) error {
	line, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("session: marshal record: %w", err)
	}
	if _, err := w.Write(line); err != nil {
		return fmt.Errorf("session: write record: %w", err)
	}
	if err := w.WriteByte('\n'); err != nil {
		return fmt.Errorf("session: write newline: %w", err)
	}
	return nil
}

// messageToRecord converts a provider.Message into the JSONL MessageRecord shape
// (the inverse of buildProviderParts), preserving text, tool calls, and tool
// responses so a checkpoint round-trips through LoadSession + ConvertToProviderHistory.
func messageToRecord(msg provider.Message) MessageRecord {
	rec := MessageRecord{
		ID:        newID(),
		Timestamp: now(),
		Content:   providerPartsToParts(msg.Parts),
	}
	if msg.Role == provider.RoleModel {
		rec.Type = MessageTypeModel
		for _, p := range msg.Parts {
			if p.FunctionCall != nil {
				rec.ToolCalls = append(rec.ToolCalls, ToolCallRecord{
					ID:     p.FunctionCall.ID,
					Name:   p.FunctionCall.Name,
					Status: "success",
				})
			}
		}
	} else {
		rec.Type = MessageTypeUser
	}
	return rec
}

// providerPartsToParts maps provider parts onto the serialisable session Part
// shape, keeping only text, function calls, and function responses.
func providerPartsToParts(parts []provider.Part) []Part {
	out := make([]Part, 0, len(parts))
	for _, p := range parts {
		switch {
		case p.Text != "":
			out = append(out, Part{Text: p.Text})
		case p.FunctionCall != nil:
			out = append(out, Part{FunctionCall: &FunctionCallPart{
				ID:   p.FunctionCall.ID,
				Name: p.FunctionCall.Name,
				Args: p.FunctionCall.Args,
			}})
		case p.FunctionResponse != nil:
			out = append(out, Part{FunctionResponse: &FuncResponsePart{
				ID:       p.FunctionResponse.CallID,
				Name:     p.FunctionResponse.Name,
				Response: p.FunctionResponse.Response,
			}})
		}
	}
	return out
}

// FormatSessionList renders a human-readable session list (used by --list-sessions).
func FormatSessionList(infos []SessionInfo) string {
	if len(infos) == 0 {
		return "No previous sessions found for this project."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\nAvailable sessions for this project (%d):\n\n", len(infos)))
	for _, s := range infos {
		current := ""
		if s.IsCurrentSession {
			current = ", current"
		}
		title := s.DisplayName
		if len(title) > 100 {
			title = title[:97] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %d. %s (%s%s) [%s]\n",
			s.Index,
			title,
			FormatRelativeTime(s.LastUpdated),
			current,
			s.ID[:8],
		))
	}
	return sb.String()
}

// SessionFilePath returns the full path to a session file given its filename.
func SessionFilePath(chatsDir, fileName string) string {
	return filepath.Join(chatsDir, fileName)
}
