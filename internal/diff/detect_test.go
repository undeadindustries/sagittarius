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
