package session

import (
	"fmt"
	"path/filepath"
	"strings"

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
