package trajectory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/session"
)

// IndexChildren scans a chats directory and returns a map of child session records keyed by sessionID
// whose parentSessionId matches the given parentSessionID.
func IndexChildren(chatsDir string, parentSessionID string) (map[string]*session.ConversationRecord, error) {
	if chatsDir == "" || parentSessionID == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read chats dir: %w", err)
	}

	children := make(map[string]*session.ConversationRecord)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(chatsDir, entry.Name())
		rec, err := session.LoadSession(path)
		if err != nil || rec == nil {
			continue
		}
		if rec.ParentSessionID == parentSessionID {
			children[rec.SessionID] = rec
		}
	}

	return children, nil
}
