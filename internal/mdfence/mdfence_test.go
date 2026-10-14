package mdfence

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		text       string
		wantKinds  []LineKind
		wantInfos  []string
		wantBodies []string
		wantClosed []bool
	}{
		{
			name:      "prose only",
			text:      "hello\nworld",
			wantKinds: []LineKind{LineProse, LineProse},
		},
		{
			name:       "plain block",
			text:       "before\n```\ncode\n```\nafter",
			wantKinds:  []LineKind{LineProse, LineFence, LineCode, LineFence, LineProse},
			wantInfos:  []string{""},
			wantBodies: []string{"code"},
			wantClosed: []bool{true},
		},
		{
			name:       "info string kept",
			text:       "```go\nfmt.Println()\n```",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence},
			wantInfos:  []string{"go"},
			wantBodies: []string{"fmt.Println()"},
			wantClosed: []bool{true},
		},
		{
			name:       "tilde fence",
			text:       "~~~\ncode\n~~~",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"code"},
			wantClosed: []bool{true},
		},
		{
			name:       "four backtick block holds triple fences",
			text:       "````\n```\ncode\n```\n````",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"```\ncode\n```"},
			wantClosed: []bool{true},
		},
		{
			name:       "markdown outer holds same-length inner fence",
			text:       "```markdown\n# Issue\n```php\n$x = 1;\n```\nend\n```",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineCode, LineCode, LineFence},
			wantInfos:  []string{"markdown"},
			wantBodies: []string{"# Issue\n```php\n$x = 1;\n```\nend"},
			wantClosed: []bool{true},
		},
		{
			name:       "non-markdown block keeps an inner info fence as content",
			text:       "```\ntext\n```php\nmore\n```\nafter",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineFence, LineProse},
			wantInfos:  []string{""},
			wantBodies: []string{"text\n```php\nmore"},
			wantClosed: []bool{true},
		},
		{
			name:       "markdown outer nested fence closed then outer closed",
			text:       "```markdown\ntext\n```php\ncode\n```\n```\nafter",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineCode, LineFence, LineProse},
			wantInfos:  []string{"markdown"},
			wantBodies: []string{"text\n```php\ncode\n```"},
			wantClosed: []bool{true},
		},
		{
			name:       "longer closing fence still closes",
			text:       "```\ncode\n````",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"code"},
			wantClosed: []bool{true},
		},
		{
			name:       "shorter closing fence is content",
			text:       "````\ncode\n```\nmore\n````",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"code\n```\nmore"},
			wantClosed: []bool{true},
		},
		{
			name:       "indented fence dedents content",
			text:       "  ```\n  code\n  ```",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"code"},
			wantClosed: []bool{true},
		},
		{
			name:      "fence indented four spaces is not a fence",
			text:      "    ```\ncode",
			wantKinds: []LineKind{LineProse, LineProse},
		},
		{
			name:       "empty block dropped",
			text:       "```\n```\n```\nreal\n```",
			wantKinds:  []LineKind{LineFence, LineFence, LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"real"},
			wantClosed: []bool{true},
		},
		{
			name:       "whitespace-only block dropped",
			text:       "```\n   \n\n```\n```\nreal\n```",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineFence, LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"real"},
			wantClosed: []bool{true},
		},
		{
			name:       "unclosed block yields its body",
			text:       "```\nbody\nmore",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode},
			wantInfos:  []string{""},
			wantBodies: []string{"body\nmore"},
			wantClosed: []bool{false},
		},
		{
			name:      "unclosed empty block dropped",
			text:      "```\n  ",
			wantKinds: []LineKind{LineFence, LineCode},
		},
		{
			name:      "backtick in info string is not a fence",
			text:      "```foo`bar\nx\n```",
			wantKinds: []LineKind{LineProse, LineProse, LineFence},
		},
		{
			name:       "tilde lines inside backtick block are content",
			text:       "```\n~~~\ncode\n~~~\n```",
			wantKinds:  []LineKind{LineFence, LineCode, LineCode, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"~~~\ncode\n~~~"},
			wantClosed: []bool{true},
		},
		{
			name:       "two blocks keep order",
			text:       "```\na\n```\nmid\n```\nb\n```",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence, LineProse, LineFence, LineCode, LineFence},
			wantInfos:  []string{"", ""},
			wantBodies: []string{"a", "b"},
			wantClosed: []bool{true, true},
		},
		{
			name:       "carriage returns trimmed",
			text:       "```\r\ncode\r\n```\r",
			wantKinds:  []LineKind{LineFence, LineCode, LineFence},
			wantInfos:  []string{""},
			wantBodies: []string{"code"},
			wantClosed: []bool{true},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lines, blocks := Parse(tc.text)

			kinds := make([]LineKind, len(lines))
			for i, l := range lines {
				kinds[i] = l.Kind
			}
			if !reflect.DeepEqual(kinds, tc.wantKinds) {
				t.Errorf("kinds = %v, want %v", kinds, tc.wantKinds)
			}

			var infos, bodies []string
			var closed []bool
			for _, b := range blocks {
				infos = append(infos, b.Info)
				bodies = append(bodies, b.Body)
				closed = append(closed, b.Closed)
			}
			if !reflect.DeepEqual(infos, tc.wantInfos) {
				t.Errorf("infos = %q, want %q", infos, tc.wantInfos)
			}
			if !reflect.DeepEqual(bodies, tc.wantBodies) {
				t.Errorf("bodies = %q, want %q", bodies, tc.wantBodies)
			}
			if !reflect.DeepEqual(closed, tc.wantClosed) {
				t.Errorf("closed = %v, want %v", closed, tc.wantClosed)
			}
		})
	}
}

// TestParseLineBlockIndices pins the remap: code lines of kept blocks carry
// the kept block index, and dropped (empty) blocks leave Block == -1.
func TestParseLineBlockIndices(t *testing.T) {
	t.Parallel()
	lines, blocks := Parse("```\n```\n```\na\n```\n```\nb\n```")
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	var codeBlocks []int
	for _, l := range lines {
		if l.Kind == LineCode {
			codeBlocks = append(codeBlocks, l.Block)
		}
	}
	want := []int{0, 1}
	if !reflect.DeepEqual(codeBlocks, want) {
		t.Errorf("code line block indices = %v, want %v", codeBlocks, want)
	}
}

// TestParseMarkdownOuterNestingNeedsInfoString pins that a bare inner fence
// inside a markdown block closes the outer block rather than nesting.
func TestParseMarkdownOuterBareInnerFenceClosesOuter(t *testing.T) {
	t.Parallel()
	_, blocks := Parse("```markdown\ntext\n```\nafter")
	if len(blocks) != 1 || blocks[0].Body != "text" || !blocks[0].Closed {
		t.Fatalf("blocks = %+v", blocks)
	}
}

func TestIsMarkdownInfo(t *testing.T) {
	t.Parallel()
	for _, info := range []string{"markdown", "md", "gfm", "Markdown", " md "} {
		if !IsMarkdownInfo(info) {
			t.Errorf("IsMarkdownInfo(%q) = false, want true", info)
		}
	}
	for _, info := range []string{"", "go", "php", "text", "markdownish"} {
		if IsMarkdownInfo(info) {
			t.Errorf("IsMarkdownInfo(%q) = true, want false", info)
		}
	}
}

// TestParseNoThirdNestingLevel pins the nesting cap: inside a markdown outer
// with a nested block already open, another info fence is plain content.
func TestParseNoThirdNestingLevel(t *testing.T) {
	t.Parallel()
	lines, blocks := Parse("```markdown\ntext\n```php\ncode\n```go\nx\n```\n```")
	if len(blocks) != 1 || !blocks[0].Closed {
		t.Fatalf("blocks = %+v", blocks)
	}
	wantBody := "text\n```php\ncode\n```go\nx\n```"
	if blocks[0].Body != wantBody {
		t.Errorf("body = %q, want %q", blocks[0].Body, wantBody)
	}
	for i, l := range lines {
		if l.Kind == LineProse {
			t.Errorf("line %d unexpectedly prose: %q", i, l.Text)
		}
	}
}

// TestParseBodiesNeverCarriageReturn is a cheap invariant for /copy: extracted
// bodies must not smuggle line endings the shell would misread.
func TestParseBodiesNeverCarriageReturn(t *testing.T) {
	t.Parallel()
	_, blocks := Parse("```\r\na\r\nb\r\n```\r\n")
	if len(blocks) != 1 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if strings.ContainsRune(blocks[0].Body, '\r') {
		t.Errorf("body contains CR: %q", blocks[0].Body)
	}
}
