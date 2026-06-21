package session

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrNoSessions is returned when no sessions exist for the project.
var ErrNoSessions = errors.New("no previous sessions found for this project")

// ErrInvalidSessionIdentifier is returned when a session identifier cannot be resolved.
type ErrInvalidSessionIdentifier struct {
	Identifier string
	ChatsDir   string
}

func (e *ErrInvalidSessionIdentifier) Error() string {
	dir := ""
	if e.ChatsDir != "" {
		dir = " in " + e.ChatsDir
	}
	return fmt.Sprintf(
		"invalid session identifier %q\n  searched for sessions%s\n  use --list-sessions to see available sessions, then --resume {number}, --resume {uuid}, or --resume latest",
		e.Identifier, dir,
	)
}

// Selector resolves resume arguments to a specific session.
type Selector struct {
	chatsDir         string
	currentSessionID string
}

// NewSelector constructs a Selector for the given chats directory.
func NewSelector(chatsDir, currentSessionID string) *Selector {
	return &Selector{chatsDir: chatsDir, currentSessionID: currentSessionID}
}

// List returns all available sessions sorted oldest-first.
func (s *Selector) List() ([]SessionInfo, error) {
	return ListSessions(s.chatsDir, s.currentSessionID)
}

// Find resolves a session by UUID or 1-based index. "latest" is handled by ResolveSession.
func (s *Selector) Find(identifier string) (*SessionInfo, error) {
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}

	id := strings.TrimSpace(identifier)

	// Try UUID match first.
	for i := range sessions {
		if sessions[i].ID == id {
			return &sessions[i], nil
		}
	}

	// Try 1-based index.
	n, err := strconv.Atoi(id)
	if err == nil && n >= 1 && n <= len(sessions) {
		return &sessions[n-1], nil
	}

	return nil, &ErrInvalidSessionIdentifier{
		Identifier: identifier,
		ChatsDir:   s.chatsDir,
	}
}

// ResolveSession resolves a resume argument ("latest", UUID, or 1-based index)
// to a loaded ConversationRecord.
func (s *Selector) ResolveSession(resumeArg string) (*SelectionResult, error) {
	trimmed := strings.TrimSpace(resumeArg)

	var info *SessionInfo
	if trimmed == ResumeLatest {
		sessions, err := s.List()
		if err != nil {
			return nil, err
		}
		if len(sessions) == 0 {
			return nil, ErrNoSessions
		}
		last := sessions[len(sessions)-1]
		info = &last
	} else {
		var err error
		info, err = s.Find(trimmed)
		if err != nil {
			return nil, err
		}
	}

	filePath := fmt.Sprintf("%s/%s", s.chatsDir, info.FileName)
	record, err := LoadSession(filePath)
	if err != nil {
		return nil, fmt.Errorf("session: load %s: %w", info.FileName, err)
	}

	displayInfo := fmt.Sprintf("Session %d: %s (%d messages, %s)",
		info.Index,
		info.FirstUserMessage,
		info.MessageCount,
		FormatRelativeTime(info.LastUpdated),
	)

	return &SelectionResult{
		SessionPath: filePath,
		Record:      record,
		DisplayInfo: displayInfo,
	}, nil
}

// FormatRelativeTime returns a human-readable relative time string.
func FormatRelativeTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}

	diff := time.Since(t)
	days := int(diff.Hours() / 24)
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	switch {
	case days > 0:
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case hours > 0:
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case minutes > 0:
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	default:
		return "Just now"
	}
}
