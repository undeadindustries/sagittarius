package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// searchFixture writes a small transcript and returns its path.
func searchFixture(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-test", session.ProjectHash(dir), "main")

	rec.RecordUserMessage("the staging database listens on port 5433")
	rec.RecordModelMessage("Noted. I will connect to 5433 for staging.", nil)
	rec.RecordUserMessage("now check the cache")
	rec.RecordModelMessage("Redis is on 6380.", nil)
	rec.RecordUserMessage("remind me about the staging port")

	return rec.FilePath()
}

func TestSearchFindsUserAndModelTurns(t *testing.T) {
	t.Parallel()

	path := searchFixture(t)
	res, err := session.Search(path, session.SearchOptions{Query: "5433"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 2 {
		t.Fatalf("Total = %d, want 2 (one user turn, one model turn)", res.Total)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("len(Matches) = %d, want 2", len(res.Matches))
	}
	// Newest first.
	if res.Matches[0].Role != session.SearchRoleModel {
		t.Errorf("first match role = %q, want the newer model turn", res.Matches[0].Role)
	}
	if res.Matches[0].TurnIndex <= res.Matches[1].TurnIndex {
		t.Errorf("turn indexes %d then %d are not newest-first",
			res.Matches[0].TurnIndex, res.Matches[1].TurnIndex)
	}
	if !strings.Contains(res.Matches[1].Excerpt, "port 5433") {
		t.Errorf("excerpt = %q, want the surrounding sentence", res.Matches[1].Excerpt)
	}
	if res.Matches[0].Timestamp == "" {
		t.Error("match carries no timestamp")
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	path := searchFixture(t)
	res, err := session.Search(path, session.SearchOptions{Query: "REDIS"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
}

func TestSearchRoleFilter(t *testing.T) {
	t.Parallel()

	path := searchFixture(t)

	tests := []struct {
		role session.SearchRole
		want int
	}{
		{role: session.SearchRoleAny, want: 2},
		{role: session.SearchRoleUser, want: 1},
		{role: session.SearchRoleModel, want: 1},
		{role: "", want: 2}, // empty means any
	}
	for _, tt := range tests {
		res, err := session.Search(path, session.SearchOptions{Query: "5433", Role: tt.role})
		if err != nil {
			t.Fatalf("Search(role=%q): %v", tt.role, err)
		}
		if res.Total != tt.want {
			t.Errorf("role %q: Total = %d, want %d", tt.role, res.Total, tt.want)
		}
	}
}

func TestSearchCapsResultsAndReportsTotal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-cap-test", session.ProjectHash(dir), "main")
	for i := 0; i < 30; i++ {
		rec.RecordUserMessage("needle appears here")
	}

	res, err := session.Search(rec.FilePath(), session.SearchOptions{Query: "needle", MaxResults: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 5 {
		t.Errorf("len(Matches) = %d, want the requested 5", len(res.Matches))
	}
	if res.Total != 30 {
		t.Errorf("Total = %d, want 30 so the caller knows the list is partial", res.Total)
	}
}

func TestSearchClampsMaxResultsToHardCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-clamp-test", session.ProjectHash(dir), "main")
	for i := 0; i < session.SearchMaxResults+10; i++ {
		rec.RecordUserMessage("needle")
	}

	res, err := session.Search(rec.FilePath(), session.SearchOptions{Query: "needle", MaxResults: 5000})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != session.SearchMaxResults {
		t.Errorf("len(Matches) = %d, want the hard cap %d", len(res.Matches), session.SearchMaxResults)
	}
}

// TestSearchSkipsOversizedLine is why the scan uses bufio.Reader rather than
// bufio.Scanner: a scanner's ErrTooLong aborts the whole pass, so one huge tool
// result would hide every match written after it.
func TestSearchSkipsOversizedLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-huge-test", session.ProjectHash(dir), "main")
	rec.RecordUserMessage("before the giant line: needle one")

	path := rec.FilePath()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	// 5 MB of a single record, past the 4 MB per-line limit.
	giant := `{"id":"x","timestamp":"t","type":"user","content":[{"text":"` +
		strings.Repeat("A", 5*1024*1024) + `"}]}` + "\n"
	if _, err := f.WriteString(giant); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	rec.RecordUserMessage("after the giant line: needle two")

	res, err := session.Search(path, session.SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.SkippedLines != 1 {
		t.Errorf("SkippedLines = %d, want 1", res.SkippedLines)
	}
	if res.Total != 2 {
		t.Fatalf("Total = %d, want both matches around the skipped line", res.Total)
	}
	if !strings.Contains(res.Matches[0].Excerpt, "needle two") {
		t.Errorf("newest match = %q, want the line written after the giant one",
			res.Matches[0].Excerpt)
	}
}

func TestSearchExcerptIsWindowed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-window-test", session.ProjectHash(dir), "main")
	rec.RecordUserMessage(strings.Repeat("x", 2000) + "NEEDLE" + strings.Repeat("y", 2000))

	res, err := session.Search(rec.FilePath(), session.SearchOptions{Query: "needle"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("len(Matches) = %d, want 1", len(res.Matches))
	}
	got := res.Matches[0].Excerpt
	if n := len([]rune(got)); n > 2*session.SearchWindowRunes+len("NEEDLE")+2 {
		t.Errorf("excerpt is %d runes, want it windowed to about %d", n, 2*session.SearchWindowRunes)
	}
	if !strings.Contains(got, "NEEDLE") {
		t.Error("excerpt does not contain the hit")
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("excerpt = %q, want elision markers on both cuts", got[:min(20, len(got))])
	}
}

// TestSearchFindsToolCallArguments covers the reason tool payloads are searched
// at all: the detail worth recovering is often a path or a port that only ever
// appeared as a tool argument.
func TestSearchFindsToolCallArguments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec := session.NewRecorder(dir, "search-toolargs-test", session.ProjectHash(dir), "main")
	rec.RecordModelMessage("", []provider.ToolCall{{
		ID:   "call-1",
		Name: "write_file",
		Args: map[string]any{"file_path": "internal/agent/scratchpad.go"},
	}})

	res, err := session.Search(rec.FilePath(), session.SearchOptions{Query: "scratchpad.go"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want the tool-call argument to match", res.Total)
	}
}

func TestSearchRejectsEmptyQuery(t *testing.T) {
	t.Parallel()

	path := searchFixture(t)
	if _, err := session.Search(path, session.SearchOptions{Query: "   "}); !errors.Is(err, session.ErrSearchEmptyQuery) {
		t.Fatalf("err = %v, want ErrSearchEmptyQuery", err)
	}
}

func TestSearchMissingFileErrors(t *testing.T) {
	t.Parallel()

	_, err := session.Search(filepath.Join(t.TempDir(), "absent.jsonl"), session.SearchOptions{Query: "x"})
	if err == nil {
		t.Fatal("Search on a missing file succeeded; want an error")
	}
}

// TestSearchEmptyPathNamesTheCause distinguishes "recording is off" from "no
// matches", which are very different answers for the model.
func TestSearchEmptyPathNamesTheCause(t *testing.T) {
	t.Parallel()

	_, err := session.Search("", session.SearchOptions{Query: "x"})
	if err == nil {
		t.Fatal("Search with no path succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "recording is disabled") {
		t.Errorf("err = %v, want it to name the cause", err)
	}
}

func TestSearchNoMatches(t *testing.T) {
	t.Parallel()

	path := searchFixture(t)
	res, err := session.Search(path, session.SearchOptions{Query: "kubernetes"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total != 0 || len(res.Matches) != 0 {
		t.Errorf("Total = %d, Matches = %d, want none", res.Total, len(res.Matches))
	}
}
