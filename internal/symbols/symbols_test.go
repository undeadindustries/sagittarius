package symbols

import "testing"

func hasDefinition(tags []Tag, name string) bool {
	for _, t := range tags {
		if t.Kind == KindDefinition && t.Name == name {
			return true
		}
	}
	return false
}

func hasReference(tags []Tag, name string) bool {
	for _, t := range tags {
		if t.Kind == KindReference && t.Name == name {
			return true
		}
	}
	return false
}

func TestTagSource(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		src      string
		wantDefs []string
		wantRefs []string
	}{
		{
			name:     "go function definition and call",
			filename: "main.go",
			src: "package main\n\n" +
				"func greet() string { return \"hi\" }\n\n" +
				"func main() { greet() }\n",
			wantDefs: []string{"greet", "main"},
			wantRefs: []string{"greet"},
		},
		{
			name:     "python function definition and call",
			filename: "app.py",
			src: "def greet():\n" +
				"    return 'hi'\n\n" +
				"def main():\n" +
				"    greet()\n",
			wantDefs: []string{"greet", "main"},
			wantRefs: []string{"greet"},
		},
		{
			name:     "javascript function definition and call",
			filename: "app.js",
			src: "function greet() { return 'hi'; }\n\n" +
				"function main() { greet(); }\n",
			wantDefs: []string{"greet", "main"},
			wantRefs: []string{"greet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags, quality, err := TagSource(tt.filename, []byte(tt.src))
			if err != nil {
				t.Fatalf("TagSource returned error: %v", err)
			}
			if quality == QualityNone {
				t.Fatalf("expected a supported language, got QualityNone")
			}
			for _, want := range tt.wantDefs {
				if !hasDefinition(tags, want) {
					t.Errorf("missing definition %q in tags %+v", want, tags)
				}
			}
			for _, want := range tt.wantRefs {
				if !hasReference(tags, want) {
					t.Errorf("missing reference %q in tags %+v", want, tags)
				}
			}
		})
	}
}

func TestTagSourceUnsupported(t *testing.T) {
	tags, quality, err := TagSource("notes.unknownext", []byte("just some prose here"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quality != QualityNone {
		t.Errorf("expected QualityNone for unknown extension, got %q", quality)
	}
	if tags != nil {
		t.Errorf("expected nil tags for unknown extension, got %+v", tags)
	}
}

func TestTagSourceBinary(t *testing.T) {
	binary := []byte("\x7fELF\x00\x00\x00\x00binary\x00content")
	tags, quality, err := TagSource("mystery.go", binary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if quality != QualityNone {
		t.Errorf("expected QualityNone for binary content, got %q", quality)
	}
	if tags != nil {
		t.Errorf("expected nil tags for binary content, got %+v", tags)
	}
}

func TestTagSourceReportsLineAndCategory(t *testing.T) {
	src := "package main\n\nfunc greet() {}\n"
	tags, _, err := TagSource("main.go", []byte(src))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, tg := range tags {
		if tg.Kind == KindDefinition && tg.Name == "greet" {
			found = true
			if tg.Line != 3 {
				t.Errorf("greet defined on line 3, got %d", tg.Line)
			}
			if tg.Language != "go" {
				t.Errorf("expected language go, got %q", tg.Language)
			}
		}
	}
	if !found {
		t.Fatal("did not find greet definition")
	}
}
