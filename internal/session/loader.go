package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadSession reads a single session JSONL file and returns a ConversationRecord.
// Returns an error if the file cannot be read or contains no valid metadata.
func LoadSession(filePath string) (*ConversationRecord, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("session: open %s: %w", filePath, err)
	}
	defer func() { _ = f.Close() }()

	var meta MetadataRecord
	var messages []MessageRecord

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1 MB line buffer
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parseLine(line, &meta, &messages)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("session: scan %s: %w", filePath, err)
	}

	if meta.SessionID == "" {
		return nil, fmt.Errorf("session: file %s has no valid metadata", filePath)
	}

	return &ConversationRecord{
		SessionID:   meta.SessionID,
		ProjectHash: meta.ProjectHash,
		StartTime:   coalesce(meta.StartTime, time.Now().UTC().Format(time.RFC3339)),
		LastUpdated: coalesce(meta.LastUpdated, time.Now().UTC().Format(time.RFC3339)),
		Summary:     meta.Summary,
		Kind:        meta.Kind,
		SessionGrants: meta.SessionGrants,
		Messages:    messages,
	}, nil
}

// parseLine interprets one JSONL line, updating meta and messages in-place.
func parseLine(line string, meta *MetadataRecord, messages *[]MessageRecord) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return // skip unparseable lines
	}

	// $rewindTo: remove messages from rewind point onwards.
	if rwRaw, ok := raw["$rewindTo"]; ok {
		var id string
		if err := json.Unmarshal(rwRaw, &id); err == nil && id != "" {
			applyRewind(messages, id)
		}
		return
	}

	// $set: partial metadata update.
	if setRaw, ok := raw["$set"]; ok {
		var setMeta MetadataRecord
		if err := json.Unmarshal(setRaw, &setMeta); err == nil {
			applyMetaUpdate(meta, &setMeta)
		}
		return
	}

	// Metadata record (has sessionId but no id field).
	if _, hasID := raw["id"]; !hasID {
		if _, hasSID := raw["sessionId"]; hasSID {
			_ = json.Unmarshal([]byte(line), meta)
			return
		}
	}

	// Message record.
	var msg MessageRecord
	if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.ID != "" {
		*messages = append(*messages, msg)
	}
}

// applyRewind removes messages from rewindID onwards (inclusive).
func applyRewind(messages *[]MessageRecord, rewindID string) {
	for i, m := range *messages {
		if m.ID == rewindID {
			*messages = (*messages)[:i]
			return
		}
	}
	// If not found, clear everything (matches fork behaviour).
	*messages = (*messages)[:0]
}

// metaEntry is a lightweight per-message view collected during the metadata-only
// scan so $rewindTo records can trim the list before counts/preview are computed,
// keeping --list-sessions consistent with LoadSession for rewound sessions.
type metaEntry struct {
	id        string
	isUser    bool
	isUOrA    bool
	firstText string
}

// trimEntriesAt removes entries from rewindID onwards (inclusive), mirroring
// applyRewind: an unknown id clears everything (matches fork behaviour).
func trimEntriesAt(entries []metaEntry, rewindID string) []metaEntry {
	for i, e := range entries {
		if e.id == rewindID {
			return entries[:i]
		}
	}
	return entries[:0]
}

// sessionLoad holds a SessionInfo plus a transient hasUOrA flag used during listing.
type sessionLoad struct {
	info    SessionInfo
	hasUOrA bool
}

// ListSessions returns all valid sessions in chatsDir sorted by StartTime (oldest first).
// Corrupted files and files with no user/assistant messages are silently skipped.
func ListSessions(chatsDir, currentSessionID string) ([]SessionInfo, error) {
	entries, err := os.ReadDir(chatsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: read dir %s: %w", chatsDir, err)
	}

	var loads []sessionLoad
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, SessionFilePrefix) {
			continue
		}
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".json") {
			continue
		}

		sl, err := loadSessionInfo(filepath.Join(chatsDir, name), currentSessionID)
		if err != nil {
			slog.Debug("session: skipping corrupted file", "file", name, "err", err)
			continue
		}
		if !sl.hasUOrA {
			continue
		}
		loads = append(loads, *sl)
	}

	// Deduplicate by session ID, keeping the most recently updated.
	loads = deduplicateLoads(loads)

	// Sort oldest-first for stable numbering.
	sort.Slice(loads, func(i, j int) bool {
		return loads[i].info.StartTime < loads[j].info.StartTime
	})

	infos := make([]SessionInfo, len(loads))
	for i, sl := range loads {
		sl.info.Index = i + 1
		infos[i] = sl.info
	}
	return infos, nil
}

// loadSessionInfo performs a fast metadata-only scan of a JSONL file.
func loadSessionInfo(filePath, currentSessionID string) (*sessionLoad, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var meta MetadataRecord
	var entries []metaEntry

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		if rwRaw, ok := raw["$rewindTo"]; ok {
			var id string
			if err := json.Unmarshal(rwRaw, &id); err == nil && id != "" {
				entries = trimEntriesAt(entries, id)
			}
			continue
		}
		if setRaw, ok := raw["$set"]; ok {
			var setMeta MetadataRecord
			if err := json.Unmarshal(setRaw, &setMeta); err == nil {
				applyMetaUpdate(&meta, &setMeta)
			}
			continue
		}
		if _, hasID := raw["id"]; !hasID {
			if _, hasSID := raw["sessionId"]; hasSID {
				_ = json.Unmarshal([]byte(line), &meta)
				continue
			}
		}

		var msg MessageRecord
		if err := json.Unmarshal([]byte(line), &msg); err == nil && msg.ID != "" {
			e := metaEntry{
				id:     msg.ID,
				isUser: msg.Type == MessageTypeUser,
				isUOrA: msg.Type == MessageTypeUser || msg.Type == MessageTypeModel,
			}
			if e.isUser {
				e.firstText = extractTextFromParts(msg.Content)
			}
			entries = append(entries, e)
		}
	}

	if meta.SessionID == "" {
		return nil, fmt.Errorf("no metadata")
	}

	var msgCount int
	var firstUserMsg string
	var hasUOrA bool
	for _, e := range entries {
		msgCount++
		if e.isUOrA {
			hasUOrA = true
		}
		if firstUserMsg == "" && e.isUser {
			firstUserMsg = e.firstText
		}
	}

	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	fileName := filepath.Base(filePath)

	isCurrentSession := false
	if currentSessionID != "" && len(currentSessionID) >= 8 {
		isCurrentSession = strings.Contains(fileName, currentSessionID[:8])
	}

	displayName := cleanMessage(firstUserMsg)
	if meta.Summary != "" {
		displayName = meta.Summary
	}
	if displayName == "" {
		displayName = "Empty conversation"
	}

	startTime := coalesce(meta.StartTime, time.Now().UTC().Format(time.RFC3339))
	lastUpdated := coalesce(meta.LastUpdated, startTime)

	return &sessionLoad{
		info: SessionInfo{
			ID:               meta.SessionID,
			File:             baseName,
			FileName:         fileName,
			StartTime:        startTime,
			LastUpdated:      lastUpdated,
			MessageCount:     msgCount,
			DisplayName:      displayName,
			FirstUserMessage: cleanMessage(firstUserMsg),
			IsCurrentSession: isCurrentSession,
		},
		hasUOrA: hasUOrA,
	}, nil
}

// DeleteSession removes the session file from chatsDir.
func DeleteSession(chatsDir, fileName string) error {
	target := filepath.Join(chatsDir, fileName)
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("session: delete %s: %w", fileName, err)
	}
	return nil
}

func applyMetaUpdate(dst, src *MetadataRecord) {
	if src.SessionID != "" {
		dst.SessionID = src.SessionID
	}
	if src.ProjectHash != "" {
		dst.ProjectHash = src.ProjectHash
	}
	if src.StartTime != "" {
		dst.StartTime = src.StartTime
	}
	if src.LastUpdated != "" {
		dst.LastUpdated = src.LastUpdated
	}
	if src.Summary != "" {
		dst.Summary = src.Summary
	}
	if src.Kind != "" {
		dst.Kind = src.Kind
	}
	if len(src.SessionGrants) > 0 {
		for _, g := range src.SessionGrants {
			found := false
			for _, existing := range dst.SessionGrants {
				if existing == g {
					found = true
					break
				}
			}
			if !found {
				dst.SessionGrants = append(dst.SessionGrants, g)
			}
		}
	}
}

func extractTextFromParts(parts []Part) string {
	var sb strings.Builder
	for _, p := range parts {
		if p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// cleanMessage sanitizes a message string for single-line display.
func cleanMessage(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func deduplicateLoads(loads []sessionLoad) []sessionLoad {
	seen := make(map[string]int, len(loads))
	out := loads[:0]
	for _, sl := range loads {
		id := sl.info.ID
		if idx, ok := seen[id]; ok {
			if sl.info.LastUpdated > out[idx].info.LastUpdated {
				out[idx] = sl
			}
		} else {
			seen[id] = len(out)
			out = append(out, sl)
		}
	}
	return out
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
