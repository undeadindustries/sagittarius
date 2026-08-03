package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEditTool(t *testing.T) {
	wsDir := t.TempDir()
	ws, err := NewWorkspace(wsDir)
	if err != nil {
		t.Fatal(err)
	}
	tool := newEditTool(ws)

	tests := []struct {
		name        string
		initial     string
		path        string
		oldStr      string
		newStr      string
		replaceAll  bool
		wantContent string
		wantErr     string
	}{
		{
			name:        "exact match",
			initial:     "hello world\nthis is a test\n",
			path:        "test.txt",
			oldStr:      "this is a test",
			newStr:      "this is replaced",
			wantContent: "hello world\nthis is replaced\n",
		},
		{
			name:        "line trimmed match",
			initial:     "func foo() {\n\treturn 1\n}\n",
			path:        "test.go",
			oldStr:      "func foo() {\n    return 1\n}",
			newStr:      "func foo() {\n\treturn 2\n}",
			wantContent: "func foo() {\n\treturn 2\n}\n",
		},
		{
			name:        "whitespace normalized match",
			initial:     "a   b\t\t c\n",
			path:        "ws.txt",
			oldStr:      "a b c",
			newStr:      "x y z",
			wantContent: "x y z\n",
		},
		{
			name:        "replace all",
			initial:     "foo bar foo",
			path:        "all.txt",
			oldStr:      "foo",
			newStr:      "baz",
			replaceAll:  true,
			wantContent: "baz bar baz",
		},
		{
			name:    "identical strings",
			initial: "foo",
			path:    "err.txt",
			oldStr:  "foo",
			newStr:  "foo",
			wantErr: "old_string and new_string are identical",
		},
		{
			name:    "ambiguous match without replace_all",
			initial: "foo foo",
			path:    "ambig.txt",
			oldStr:  "foo",
			newStr:  "bar",
			wantErr: "multiple matches",
		},
		{
			name:        "create new file",
			initial:     "",
			path:        "new.txt",
			oldStr:      "",
			newStr:      "new content",
			wantContent: "new content",
		},
		{
			name:    "empty old string on existing file",
			initial: "existing",
			path:    "exists.txt",
			oldStr:  "",
			newStr:  "new content",
			wantErr: "old_string is empty but the file exists",
		},
		{
			name:        "CRLF normalization",
			initial:     "a\r\nb\r\n",
			path:        "crlf.txt",
			oldStr:      "a\nb",
			newStr:      "a\nc",
			wantContent: "a\r\nc\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absPath := filepath.Join(wsDir, tt.path)
			if tt.initial != "" || (tt.initial == "" && tt.wantErr != "" && tt.oldStr == "") {
				// Create file
				_ = os.WriteFile(absPath, []byte(tt.initial), 0o644)
			}

			args := map[string]any{
				ParamFilePath:      tt.path,
				EditParamOldString: tt.oldStr,
				EditParamNewString: tt.newStr,
			}
			if tt.replaceAll {
				args[EditParamReplaceAll] = true
			}

			_, err := tool.Execute(context.Background(), args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, _ := os.ReadFile(absPath)
				if string(got) != tt.wantContent {
					t.Errorf("content = %q, want %q", string(got), tt.wantContent)
				}
			}
		})
	}
}
