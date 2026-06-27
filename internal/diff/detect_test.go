package diff

import "testing"

func TestLooksLikeUnifiedDiff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "header",
			text: "--- a/foo.js\n+++ b/foo.js\n@@ -1,3 +1,2 @@\n-old\n+new",
			want: true,
		},
		{
			name: "pseudo hunk",
			text: "-  goBack() {\n-    this.render();\n+      this.os.openApp(file.name);\n   }",
			want: true,
		},
		{
			name: "normal js",
			text: "class FileExplorer {\n  goBack() {\n    this.render();\n  }\n}\nwindow.FileExplorer = FileExplorer;",
			want: false,
		},
		{
			name: "markdown list only",
			text: "- item one\n- item two\n- item three",
			want: false,
		},
		{
			name: "markdown long bullet list",
			text: "- one\n- two\n- three\n- four\n- five\n- six",
			want: false,
		},
		{
			name: "css vendor prefixes",
			text: ".window {\n  -webkit-backdrop-filter: blur(20px);\n  backdrop-filter: blur(20px);\n  -webkit-box-shadow: 0 10px 30px rgba(0,0,0,0.3);\n  -webkit-transform: translateZ(0);\n  -moz-user-select: none;\n  -webkit-user-select: none;\n}",
			want: false,
		},
		{
			name: "whole new file pasted as additions",
			text: "+import x\n+\n+function foo() {\n+  return 1;\n+}",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := LooksLikeUnifiedDiff(tc.text); got != tc.want {
				t.Fatalf("LooksLikeUnifiedDiff() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLooksLikeEjectionMarker(t *testing.T) {
	t.Parallel()
	tag := "<file_written path=\"js/apps/snake.js\" lines=166 tokens=1296 cached=true>"
	if !LooksLikeEjectionMarker(tag) {
		t.Fatal("expected ejection marker detection")
	}
	if LooksLikeEjectionMarker("class Snake {}\n") {
		t.Fatal("did not expect ejection marker on real code")
	}
}

func TestLooksLikePlaceholderContent(t *testing.T) {
	t.Parallel()
	if !LooksLikePlaceholderContent("foo\n// ... existing code ...\nbar") {
		t.Fatal("expected placeholder detection")
	}
	if LooksLikePlaceholderContent("const x = 1;\nconst y = 2;") {
		t.Fatal("did not expect placeholder detection")
	}
}
