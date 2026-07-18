package symbols

import (
	"bytes"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// Quality reports how completely a source buffer could be analyzed.
type Quality string

const (
	// QualityNone means the language was not recognized, has no tags query, or
	// the buffer is not text (binary). No tags are produced.
	QualityNone Quality = "none"
	// QualityPartial means the language parsed without its external scanner;
	// some tags may be missing.
	QualityPartial Quality = "partial"
	// QualityFull means the parser had every component the grammar requires.
	QualityFull Quality = "full"
)

// Kind classifies a tag as a symbol definition or a reference to one.
const (
	KindDefinition = "definition"
	KindReference  = "reference"
)

// Tag is a single symbol occurrence extracted from a source buffer. Line and
// Column are 1-based, matching editor and grep conventions.
type Tag struct {
	// Name is the captured symbol text (e.g. the function identifier).
	Name string
	// Kind is either KindDefinition or KindReference.
	Kind string
	// Category is the tree-sitter tag suffix (e.g. "function", "class",
	// "call"), derived from the grammar's tags query. May be empty.
	Category string
	// Language is the detected grammar name (e.g. "go", "python").
	Language string
	// Path is the file the tag came from. TagSource leaves it empty (it parses a
	// single buffer); callers that scan multiple files set it.
	Path string
	// Line is the 1-based line of the symbol name.
	Line int
	// Column is the 1-based column of the symbol name.
	Column int
}

// TagSource extracts definitions and references from src, using filename only
// to detect the language. It never returns a hard error for an unrecognized or
// binary buffer: such inputs yield (nil, QualityNone, nil) so directory scans
// can skip them silently. The returned error is reserved for genuinely
// unexpected failures and is currently always nil.
func TagSource(filename string, src []byte) ([]Tag, Quality, error) {
	if len(src) == 0 || isBinary(src) {
		return nil, QualityNone, nil
	}
	entry := grammars.DetectLanguage(filename)
	if entry == nil {
		return nil, QualityNone, nil
	}
	query := grammars.ResolveTagsQuery(*entry)
	if strings.TrimSpace(query) == "" {
		return nil, QualityNone, nil
	}
	lang := entry.Language()
	if lang == nil {
		return nil, QualityNone, nil
	}
	tagger, err := gotreesitter.NewTagger(lang, query)
	if err != nil {
		// A grammar without a usable tags query is not a caller error; skip it.
		return nil, QualityNone, nil
	}

	raw := tagger.Tag(src)
	tags := make([]Tag, 0, len(raw))
	for _, t := range raw {
		kind, category := splitKind(t.Kind)
		if kind == "" {
			continue
		}
		tags = append(tags, Tag{
			Name:     t.Name,
			Kind:     kind,
			Category: category,
			Language: entry.Name,
			Line:     int(t.NameRange.StartPoint.Row) + 1,
			Column:   int(t.NameRange.StartPoint.Column) + 1,
		})
	}
	return tags, resolveQuality(entry), nil
}

// splitKind maps a tree-sitter tag kind ("definition.function",
// "reference.call") to (KindDefinition|KindReference, category). Unknown
// prefixes yield an empty kind so the caller can drop the tag.
func splitKind(raw string) (kind, category string) {
	prefix, suffix, found := strings.Cut(raw, ".")
	switch prefix {
	case KindDefinition:
		kind = KindDefinition
	case KindReference:
		kind = KindReference
	default:
		return "", ""
	}
	if found {
		category = suffix
	}
	return kind, category
}

// resolveQuality maps the grammar registry's parse quality onto the local
// Quality type. An unset registry quality is treated as full because a resolved
// tags query implies the grammar loaded.
func resolveQuality(entry *grammars.LangEntry) Quality {
	switch entry.Quality {
	case grammars.ParseQualityNone:
		return QualityNone
	case grammars.ParseQualityPartial:
		return QualityPartial
	default:
		return QualityFull
	}
}

// isBinary reports whether src looks like non-text content. A NUL byte in the
// leading window is a reliable, cheap signal used by grep tools and editors.
func isBinary(src []byte) bool {
	const window = 8000
	head := src
	if len(head) > window {
		head = head[:window]
	}
	return bytes.IndexByte(head, 0) >= 0
}
