package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
	"github.com/undeadindustries/sagittarius/internal/diff"
)

// changedLinesFunc reports which 1-based lines of a reported file this
// session changed. known is false when that can't be determined (no
// snapshot, file too large), and the caller then keeps the finding.
type changedLinesFunc func(path string) (changed map[int]bool, known bool)

// findingLocationRe matches a "path:line" location at the start of a
// diagnostic line, optionally after rustc/ruff's "-->" arrow. The path must
// contain a dot or a separator (checked by the caller) so eslint's
// "  8:5  error" rows aren't read as a file named "8".
var findingLocationRe = regexp.MustCompile(`^\s*(?:-->\s*)?(\S+?):(\d+)(?::\d+)?(?::|\s|$)`)

// findingSummaryRe matches the tally lines linters print after their
// diagnostics. They carry no location and can be dropped when a finding is
// split, since their counts no longer apply.
var findingSummaryRe = regexp.MustCompile(`^\s*(Found \d+ |\[\*\] |No fixes available|\d+ (problems?|errors?|warnings?)\b)`)

// splitPreexistingFindings separates linter findings on lines this session
// didn't change from the rest. A linter reports the whole file, so without
// this the model is told to fix problems that were there before it started,
// which pulls it into out-of-scope edits.
//
// Only SeverityWarning findings are considered: compile and type errors stay,
// since an edit can break code on lines it never touched. A finding with no
// parseable location is kept whole. A finding that mixes old and new
// locations is split only when every line is a location or tally line;
// multi-line formats can't be split safely and are kept whole.
func splitPreexistingFindings(findings []diagnostics.Finding, changed changedLinesFunc) (kept, preexisting []diagnostics.Finding) {
	if changed == nil {
		return findings, nil
	}
	for _, f := range findings {
		if f.Severity != diagnostics.SeverityWarning {
			kept = append(kept, f)
			continue
		}
		in, out, lineOriented := classifyFindingLines(f.Message, changed)
		switch {
		case len(out) == 0:
			kept = append(kept, f)
		case len(in) == 0:
			preexisting = append(preexisting, f)
		case lineOriented:
			kept = append(kept, diagnostics.Finding{Tool: f.Tool, Severity: f.Severity, Message: strings.Join(in, "\n")})
			preexisting = append(preexisting, diagnostics.Finding{Tool: f.Tool, Severity: f.Severity, Message: strings.Join(out, "\n")})
		default:
			kept = append(kept, f)
		}
	}
	return kept, preexisting
}

// classifyFindingLines sorts a finding's location lines into those on changed
// (or undeterminable) lines and those on unchanged ones. lineOriented reports
// whether every non-blank line is a location or tally line.
func classifyFindingLines(message string, changed changedLinesFunc) (in, out []string, lineOriented bool) {
	lineOriented = true
	for _, line := range strings.Split(message, "\n") {
		if strings.TrimSpace(line) == "" || findingSummaryRe.MatchString(line) {
			continue
		}
		path, lineNo, ok := parseFindingLocation(line)
		if !ok {
			lineOriented = false
			continue
		}
		lines, known := changed(path)
		if !known || lines[lineNo] {
			in = append(in, line)
		} else {
			out = append(out, line)
		}
	}
	return in, out, lineOriented
}

func parseFindingLocation(line string) (path string, lineNo int, ok bool) {
	m := findingLocationRe.FindStringSubmatch(line)
	if m == nil || !strings.ContainsAny(m[1], `./\`) {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	return m[1], n, true
}

// matchSessionFile maps a path as a tool printed it (absolute, relative to
// the project root, or relative to the tool's working directory) to a file
// the session changed. It returns "" when none matches.
func matchSessionFile(reported string, sessionFiles []string) string {
	abs := filepath.IsAbs(reported)
	p := filepath.ToSlash(filepath.Clean(reported))
	for _, rel := range sessionFiles {
		rel = filepath.ToSlash(rel)
		switch {
		case p == rel:
			return rel
		case abs && strings.HasSuffix(p, "/"+rel):
			return rel
		case !abs && strings.HasSuffix(rel, "/"+p):
			return rel // relative to a subdirectory the tool ran in
		}
	}
	return ""
}

// sessionChangedLines builds the changedLinesFunc for one check run from the
// snapshot manager. A changed file is diffed from its pre-session content to
// what is on disk now, which also covers edits made outside write_file (a
// formatter run from the shell). A path that matches no session file is
// unknown rather than unchanged: a mismatched path format must not hide a
// finding the edit caused. Results are cached per file for the run.
func (r *Runner) sessionChangedLines() changedLinesFunc {
	if r.snap == nil {
		return nil
	}
	sessionFiles := r.snap.ChangedFiles()
	type result struct {
		lines map[int]bool
		known bool
	}
	cache := map[string]result{}
	return func(path string) (map[int]bool, bool) {
		rel := matchSessionFile(path, sessionFiles)
		if rel == "" {
			return nil, false
		}
		if res, hit := cache[rel]; hit {
			return res.lines, res.known
		}
		res := result{}
		if base := r.snap.SessionBaseline(rel); base.Captured {
			if current, err := os.ReadFile(filepath.Join(r.workDir, filepath.FromSlash(rel))); err == nil {
				res.lines, res.known = diff.ChangedLines(base.Content, string(current))
			}
		}
		cache[rel] = res
		return res.lines, res.known
	}
}
