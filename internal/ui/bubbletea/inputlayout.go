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

// inputBoxHeight returns the textarea viewport height that keeps all wrapped
// content visible while typing. Bubbles' textarea always appends m.height blank
// "end of buffer" rows to its content and then scrolls to keep the cursor in
// view; when height equals the wrapped line count the first line is pushed out
// of the viewport. Reserve one extra row (+1) so the cursor on the last content
// line still leaves earlier lines visible. Clamp to maxInputRows.
func inputBoxHeight(contentLines int) int {
	if contentLines <= 0 {
		return 1
	}
	h := contentLines + 1
	if h > maxInputRows {
		return maxInputRows
	}
	return h
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

	// When not capped, keep the viewport pinned to the top so wrapped line 1
	// stays visible. At the cap we allow the widget's own scroll behaviour.
	if h == contentLines+1 {
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
