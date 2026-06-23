// Package atmention implements gemini-cli-style "@path/to/file" references in
// user input. When a user writes "explain @internal/agent/app.go", the named
// file is read from the workspace and its content is injected into the message
// sent to the model, while the scrollback keeps showing the raw text the user
// typed.
//
// The package is a leaf: it depends only on internal/provider, internal/tools
// (for workspace path validation), and internal/ui (for completion types). The
// agent layer calls Expand before appending the user turn to history; the TUI
// calls (*Index).Complete for inline path autocompletion.
package atmention

import "strings"

// A mention is recognised only when the '@' begins a whitespace-delimited token
// (start of input or preceded by ASCII whitespace). This avoids treating the
// '@' in email addresses or decorators (e.g. "rob@example.com") as a file
// reference, while still surfacing genuine typos in intended mentions.

// scanMentions returns the unescaped path strings referenced by unescaped '@'
// tokens in query, in order of appearance. Escaped "\@" is ignored.
func scanMentions(query string) []string {
	runes := []rune(query)
	var paths []string
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if isEscaped(runes, i) || !isMentionStart(runes, i) {
			continue
		}
		path, next := scanPath(runes, i+1)
		if path != "" {
			paths = append(paths, path)
		}
		i = next - 1
	}
	return paths
}

// isMentionStart reports whether the '@' at index i begins a fresh token: it is
// at the start of input, or preceded by whitespace or a path delimiter (e.g.
// "(@a.go)"). This excludes an '@' embedded after an alphanumeric character, so
// email addresses like "rob@example.com" are not treated as references.
func isMentionStart(runes []rune, i int) bool {
	if i == 0 {
		return true
	}
	return isDelimiter(runes[i-1])
}

// isEscaped reports whether the rune at index i is preceded by an odd number of
// backslashes (and is therefore escaped).
func isEscaped(runes []rune, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && runes[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// scanPath reads a path token beginning at start (the index just after '@') and
// returns the unescaped path plus the index just past the token. A leading
// double quote reads a quoted path verbatim (allowing spaces); otherwise the
// token runs until an unescaped delimiter.
func scanPath(runes []rune, start int) (string, int) {
	if start < len(runes) && runes[start] == '"' {
		var b strings.Builder
		i := start + 1
		for i < len(runes) && runes[i] != '"' {
			b.WriteRune(runes[i])
			i++
		}
		if i < len(runes) {
			i++ // consume closing quote
		}
		return b.String(), i
	}

	var b strings.Builder
	i := start
	for i < len(runes) {
		c := runes[i]
		if c == '\\' && i+1 < len(runes) {
			b.WriteRune(runes[i+1])
			i += 2
			continue
		}
		if isDelimiter(c) {
			break
		}
		if c == '.' && (i+1 >= len(runes) || isSpace(runes[i+1])) {
			// A trailing '.' (end of input or before whitespace) ends sentences,
			// so it is not part of the path. An interior '.' (e.g. "a.go") is.
			break
		}
		b.WriteRune(c)
		i++
	}
	return b.String(), i
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// isDelimiter reports whether r ends a path token. It mirrors gemini-cli's set:
// ASCII whitespace plus a handful of punctuation that rarely appears in paths.
func isDelimiter(r rune) bool {
	if isSpace(r) {
		return true
	}
	switch r {
	case ',', ';', '!', '?', '(', ')', '[', ']', '{', '}':
		return true
	}
	return false
}

// unescape collapses backslash escapes ("\x" -> "x") in a partial path token,
// used for matching completion candidates.
func unescape(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			b.WriteRune(runes[i+1])
			i++
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// escape backslash-escapes spaces in a path so it round-trips through scanPath
// as a single token when inserted by autocompletion.
func escape(s string) string {
	if !strings.ContainsAny(s, " \t") {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '\t' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
