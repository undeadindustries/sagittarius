package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	// SearchDefaultMaxResults is the result count used when a caller does not
	// ask for one.
	SearchDefaultMaxResults = 20
	// SearchMaxResults is the hard ceiling. Fifty windows of ~600 runes keeps
	// worst-case output near 30 KB, under the 64 KiB tool-output spill
	// threshold, so a search never produces a spill artifact.
	SearchMaxResults = 50
	// SearchWindowRunes is how much text surrounds a hit on each side.
	SearchWindowRunes = 300

	// searchReadBufferBytes sizes the streaming reader.
	searchReadBufferBytes = 64 * 1024
	// searchMaxLineBytes bounds what one pathological line can cost. A session
	// line can carry a whole file body; past this the line is skipped and
	// counted rather than buffered.
	searchMaxLineBytes = 4 * 1024 * 1024
)

// Message-record markers used to identify a conversation line without paying
// for a json.Unmarshal. Recorder.appendLineLocked writes with json.Marshal,
// which emits compact JSON, so the no-space form is exact.
const (
	userLineMarker  = `"type":"` + string(MessageTypeUser) + `"`
	modelLineMarker = `"type":"` + string(MessageTypeModel) + `"`
)

// SearchRole filters matches by who said it.
type SearchRole string

const (
	SearchRoleAny   SearchRole = "any"
	SearchRoleUser  SearchRole = "user"
	SearchRoleModel SearchRole = "model"
)

// ErrSearchEmptyQuery is returned for a blank query rather than matching every
// line, which would hand back an arbitrary tail of the conversation.
var ErrSearchEmptyQuery = errors.New("session search: query is empty")

// errLineTooLong marks a line that exceeded searchMaxLineBytes. It is reported
// per line and skipped, never surfaced as a scan failure: one oversized tool
// result must not hide every match after it.
var errLineTooLong = errors.New("session search: line exceeds the size limit")

// SearchOptions configures Search.
type SearchOptions struct {
	// Query is a literal, case-insensitive substring. Not a regexp: the caller
	// is a language model recovering a remembered detail, and a malformed
	// pattern error is a worse outcome than a literal miss.
	Query string
	// Role restricts matches. Empty means SearchRoleAny.
	Role SearchRole
	// MaxResults caps the returned slice, clamped to SearchMaxResults.
	MaxResults int
}

// Match is one hit in the session transcript.
type Match struct {
	// TurnIndex is the ordinal of the message within the transcript, from 1.
	TurnIndex int
	Role      SearchRole
	Timestamp string
	// Excerpt is a window of at most 2*SearchWindowRunes runes around the hit,
	// marked with an ellipsis where it was cut.
	Excerpt string
}

// SearchResult reports what Search found and what it had to skip.
type SearchResult struct {
	Matches []Match
	// Total counts every match found, so a caller can say "showing 20 of 57"
	// instead of implying the list is complete.
	Total int
	// SkippedLines counts lines too large to buffer. Non-zero means the result
	// may be incomplete, which is worth telling the model.
	SkippedLines int
}

// Search scans a session JSONL file for a literal substring and returns the
// most recent matches, newest first.
//
// It is a pure function over a file path, so it is testable without a runner or
// a recorder. The scan is deliberately index-free: one streaming pass, no
// cache, no watcher, no background goroutine. A session file is append-only and
// read a handful of times per conversation, so an index would cost more to
// maintain than the scan costs to run.
func Search(path string, opts SearchOptions) (SearchResult, error) {
	query := strings.TrimSpace(opts.Query)
	if query == "" {
		return SearchResult{}, ErrSearchEmptyQuery
	}
	if path == "" {
		return SearchResult{}, errors.New("session search: no session file (recording is disabled)")
	}

	role := opts.Role
	if role == "" {
		role = SearchRoleAny
	}
	limit := opts.MaxResults
	if limit <= 0 {
		limit = SearchDefaultMaxResults
	}
	limit = min(limit, SearchMaxResults)

	f, err := os.Open(path)
	if err != nil {
		return SearchResult{}, fmt.Errorf("session search: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	needle := strings.ToLower(query)
	reader := bufio.NewReaderSize(f, searchReadBufferBytes)
	result := SearchResult{}
	turn := 0

	for {
		line, readErr := readSessionLine(reader)

		// The marker check is a plain substring scan on the raw line, so
		// metadata and $set lines cost almost nothing and only conversation
		// lines pay for the lowercase copy below.
		if isMessageLine(line) {
			turn++
			if strings.Contains(strings.ToLower(line), needle) {
				if m, ok := matchLine(line, turn, query, needle, role); ok {
					result.Total++
					result.Matches = append(result.Matches, m)
				}
			}
		}

		switch {
		case readErr == nil:
			continue
		case errors.Is(readErr, errLineTooLong):
			result.SkippedLines++
		case errors.Is(readErr, io.EOF):
			// Newest first: the caller is recovering recent context far more
			// often than ancient context, and the cap has to drop something.
			if len(result.Matches) > limit {
				result.Matches = result.Matches[len(result.Matches)-limit:]
			}
			reverseMatches(result.Matches)
			return result, nil
		default:
			return result, fmt.Errorf("session search: read %s: %w", path, readErr)
		}
	}
}

// readSessionLine reads one newline-terminated line, bounded by
// searchMaxLineBytes. bufio.Scanner is deliberately not used: its ErrTooLong
// aborts the entire scan, so one oversized line would hide every later match.
func readSessionLine(r *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		chunk, err := r.ReadSlice('\n')
		b.Write(chunk)
		if errors.Is(err, bufio.ErrBufferFull) {
			if b.Len() > searchMaxLineBytes {
				discardLine(r)
				return "", errLineTooLong
			}
			continue
		}
		if err != nil {
			return strings.TrimSpace(b.String()), err
		}
		return strings.TrimSpace(b.String()), nil
	}
}

// discardLine consumes bytes up to and including the next newline so the reader
// resumes at a record boundary.
func discardLine(r *bufio.Reader) {
	for {
		if _, err := r.ReadSlice('\n'); !errors.Is(err, bufio.ErrBufferFull) {
			return
		}
	}
}

// isMessageLine reports whether a raw line is a user or model turn. Info, error,
// and warning records are UI notices rather than conversation, and metadata and
// $set lines carry no type at all.
func isMessageLine(line string) bool {
	return strings.Contains(line, userLineMarker) || strings.Contains(line, modelLineMarker)
}

// matchLine decodes a line whose raw text matched and extracts the excerpt. The
// raw hit can be a false positive — the needle may have landed in an id or a
// tool-call name — so the flattened text is re-checked here.
func matchLine(line string, turn int, query, needle string, want SearchRole) (Match, bool) {
	var rec MessageRecord
	if err := json.Unmarshal([]byte(line), &rec); err != nil {
		return Match{}, false
	}
	role := recordRole(rec)
	if role == "" || (want != SearchRoleAny && role != want) {
		return Match{}, false
	}

	text := messageText(rec)
	idx := strings.Index(strings.ToLower(text), needle)
	if idx < 0 {
		return Match{}, false
	}

	return Match{
		TurnIndex: turn,
		Role:      role,
		Timestamp: rec.Timestamp,
		Excerpt:   excerpt(text, idx, len(query)),
	}, true
}

func recordRole(rec MessageRecord) SearchRole {
	switch rec.Type {
	case MessageTypeUser:
		return SearchRoleUser
	case MessageTypeModel:
		return SearchRoleModel
	default:
		return ""
	}
}

// messageText flattens a record's parts into searchable text. Tool calls and
// responses are included because the detail being recovered is often an
// argument or a result — a path, a port, a command — rather than prose.
func messageText(rec MessageRecord) string {
	var b strings.Builder
	write := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	for _, p := range rec.Content {
		write(p.Text)
		if p.FunctionCall != nil {
			write(p.FunctionCall.Name + " " + marshalCompact(p.FunctionCall.Args))
		}
		if p.FunctionResponse != nil {
			write(p.FunctionResponse.Name + " " + marshalCompact(p.FunctionResponse.Response))
		}
	}
	return b.String()
}

// marshalCompact renders a tool payload for searching, yielding "" rather than
// an error string for anything that will not serialize.
func marshalCompact(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// excerpt returns a rune-safe window around a byte offset, marking each cut.
func excerpt(text string, byteIdx, matchLen int) string {
	runes := []rune(text)
	hit := len([]rune(text[:byteIdx]))
	hitEnd := hit + len([]rune(text[byteIdx:min(byteIdx+matchLen, len(text))]))

	start := max(hit-SearchWindowRunes, 0)
	end := min(hitEnd+SearchWindowRunes, len(runes))

	var b strings.Builder
	if start > 0 {
		b.WriteString("…")
	}
	b.WriteString(strings.TrimSpace(string(runes[start:end])))
	if end < len(runes) {
		b.WriteString("…")
	}
	return b.String()
}

func reverseMatches(m []Match) {
	for i, j := 0, len(m)-1; i < j; i, j = i+1, j-1 {
		m[i], m[j] = m[j], m[i]
	}
}
