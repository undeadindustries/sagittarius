package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Keys attached when tool-call argument JSON cannot be decoded even after
// salvage. The scheduler surfaces them as INVALID_ARGS instead of claiming
// the model sent no parameters.
const (
	ToolArgParseErrorKey   = "_parse_error"
	ToolArgRawArgumentsKey = "_raw_arguments"
)

const maxRawArgPreviewRunes = 200

// UnmarshalToolArguments decodes a model-emitted tool-call arguments payload.
// The fast path is ordinary json.Unmarshal. When that fails — typically
// because a streamed Qwen/vLLM call concatenated a literal newline inside a
// JSON string — a salvage pass strips markdown fences, drops trailing commas,
// and escapes raw control characters inside strings, then retries.
//
// A payload that is still not an object is returned as a diagnostic map
// rather than silently becoming {}. Empty or whitespace-only input is {}.
func UnmarshalToolArguments(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	if args, err := unmarshalObject(raw); err == nil {
		return args
	}
	args, err := salvageToolArguments(raw)
	if err == nil {
		return args
	}
	return map[string]any{
		ToolArgParseErrorKey:   err.Error(),
		ToolArgRawArgumentsKey: capRunes(raw, maxRawArgPreviewRunes),
	}
}

func salvageToolArguments(raw string) (map[string]any, error) {
	cleaned := stripMarkdownFence(raw)
	cleaned = stripTrailingCommas(cleaned)
	cleaned = escapeControlCharsInStrings(cleaned)
	return unmarshalObject(cleaned)
}

func unmarshalObject(raw string) (map[string]any, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, err
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return strings.Trim(s, "`")
	}
	body := strings.TrimSpace(s[nl+1:])
	if i := strings.LastIndex(body, "```"); i >= 0 {
		body = strings.TrimSpace(body[:i])
	}
	return body
}

func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inString && c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if !inString && c == ',' {
			j := i + 1
			for j < len(s) && isJSONSpace(s[j]) {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func escapeControlCharsInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inString && c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString && c < 0x20 {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				fmt.Fprintf(&b, `\u%04x`, c)
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isJSONSpace(c byte) bool {
	return c == ' ' || c == '\n' || c == '\r' || c == '\t'
}

func capRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for range n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i] + "…"
}
