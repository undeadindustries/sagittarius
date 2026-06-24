package bubbletea

import (
	"reflect"
	"strings"
	"unsafe"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
)

// inputContentLines counts how many display rows the current input value occupies
// when soft-wrapped at the textarea's active text width.
func inputContentLines(input textarea.Model) int {
	w := input.Width()
	if w <= 0 {
		return 1
	}
	lines := 0
	for _, hard := range strings.Split(input.Value(), "\n") {
		lines += wordWrapLineCount(hard, w)
	}
	if lines < 1 {
		return 1
	}
	return lines
}

// inputBoxHeight returns the textarea viewport height for the given number of
// wrapped content lines. The box grows one row per content line, clamped to
// maxInputRows; beyond the cap the textarea scrolls internally. A single,
// per-line prompt is rendered via SetPromptFunc (continuation rows are blank),
// so no extra "+1" buffer row is needed to avoid a duplicate prompt prefix.
func inputBoxHeight(contentLines int) int {
	if contentLines <= 1 {
		return 1
	}
	if contentLines > maxInputRows {
		return maxInputRows
	}
	return contentLines
}

// syncInputLayout sizes the textarea to the terminal width and the computed box
// height, then resets internal scroll when the full prompt fits without needing
// a clipped viewport.
func (m *model) syncInputLayout() {
	w := max(m.width-2, 1)
	m.input.SetWidth(w)

	contentLines := inputContentLines(m.input)
	h := inputBoxHeight(contentLines)
	m.input.SetHeight(h)

	// While the whole prompt fits in the box, keep the widget's internal
	// viewport pinned to the top so wrapped line 1 stays visible. Once the
	// content exceeds the cap we allow the widget's own scroll behaviour to
	// follow the cursor.
	if contentLines <= maxInputRows {
		inputScrollToTop(&m.input)
	}
}

// inputScrollToTop resets the textarea's internal viewport to the first row.
// The bubbles textarea does not expose its viewport; repositionView runs during
// Update with a stale height and can leave YOffset > 0, hiding earlier lines.
func inputScrollToTop(input *textarea.Model) {
	rv := reflect.ValueOf(input).Elem()
	field := rv.FieldByName("viewport")
	if !field.IsValid() || field.IsNil() {
		return
	}
	// viewport is an unexported field; NewAt is required to reach it.
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	vp, ok := field.Interface().(*viewport.Model)
	if !ok || vp == nil {
		return
	}
	vp.GotoTop()
}

// visibleInputPrefix returns the first word of s, used by tests to assert the
// top of a wrapped prompt stayed on screen.
func visibleInputPrefix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}
