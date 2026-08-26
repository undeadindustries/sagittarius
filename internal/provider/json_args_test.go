package provider

import (
	"strings"
	"testing"
)

func TestUnmarshalToolArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		want      map[string]any
		parseFail bool
	}{
		{
			name: "valid json fast path",
			raw:  `{"file_path":"a.txt","content":"hi"}`,
			want: map[string]any{"file_path": "a.txt", "content": "hi"},
		},
		{
			name: "empty string",
			raw:  "",
			want: map[string]any{},
		},
		{
			name: "whitespace only",
			raw:  "   \n",
			want: map[string]any{},
		},
		{
			name: "json null is empty object",
			raw:  "null",
			want: map[string]any{},
		},
		{
			name: "raw newlines inside string",
			raw:  "{\"file_path\":\"a.py\",\"content\":\"def foo():\n    return 1\n\"}",
			want: map[string]any{"file_path": "a.py", "content": "def foo():\n    return 1\n"},
		},
		{
			name: "raw crlf and tab inside string",
			raw:  "{\"content\":\"line1\r\n\tindented\"}",
			want: map[string]any{"content": "line1\r\n\tindented"},
		},
		{
			name: "trailing comma",
			raw:  `{"file_path":"a.txt","content":"hi",}`,
			want: map[string]any{"file_path": "a.txt", "content": "hi"},
		},
		{
			name: "markdown fence",
			raw:  "```json\n{\"file_path\":\"a.txt\",\"content\":\"hi\"}\n```",
			want: map[string]any{"file_path": "a.txt", "content": "hi"},
		},
		{
			name: "fence plus raw newline",
			raw:  "```json\n{\"file_path\":\"a.py\",\"content\":\"def foo():\n    pass\n\"}\n```",
			want: map[string]any{"file_path": "a.py", "content": "def foo():\n    pass\n"},
		},
		{
			name:      "unclosed object",
			raw:       `{"file_path":"a.txt","content":"hi"`,
			parseFail: true,
		},
		{
			name:      "array is not an object",
			raw:       `["file_path"]`,
			parseFail: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := UnmarshalToolArguments(tc.raw)
			if tc.parseFail {
				if _, ok := got[ToolArgParseErrorKey]; !ok {
					t.Fatalf("args = %v, want %q set", got, ToolArgParseErrorKey)
				}
				preview, _ := got[ToolArgRawArgumentsKey].(string)
				if preview == "" {
					t.Fatal("expected a raw-arguments preview")
				}
				if strings.Contains(preview, "\x00") {
					t.Fatal("preview must not invent NULs")
				}
				return
			}
			if _, ok := got[ToolArgParseErrorKey]; ok {
				t.Fatalf("unexpected parse error: %v", got)
			}
			assertStringMap(t, got, tc.want)
		})
	}
}

func TestUnmarshalToolArgumentsCapsPreview(t *testing.T) {
	t.Parallel()
	raw := `{"broken":` + strings.Repeat("x", 400)
	got := UnmarshalToolArguments(raw)
	preview, _ := got[ToolArgRawArgumentsKey].(string)
	if got := []rune(preview); len(got) != maxRawArgPreviewRunes+1 || got[len(got)-1] != '…' {
		t.Fatalf("preview runes = %d, want %d plus ellipsis (got %q)", len([]rune(preview)), maxRawArgPreviewRunes, preview)
	}
}

func assertStringMap(t *testing.T, got, want map[string]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for k, wantV := range want {
		gotV, ok := got[k]
		if !ok {
			t.Fatalf("args = %v, missing %q", got, k)
		}
		if gotV != wantV {
			t.Fatalf("args[%q] = %v, want %v", k, gotV, wantV)
		}
	}
}
