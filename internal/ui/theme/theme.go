// Package theme centralizes Sagittarius TUI colors behind a small set of
// semantic styles. The agent layer never imports it; only the Bubble Tea
// renderer and dialog overlays do (preserving the AD-004 UI-library boundary).
//
// Two built-in themes ship today: a purple-leaning Default for color terminals
// and a Greyscale theme (black/white/grey only) selected via NO_COLOR or the
// settings key ui.theme: "greyscale". Greyscale styles use bold/faint/reverse
// attributes only and never emit foreground/background color codes, so the
// output stays monochrome on any terminal.
package theme

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a resolved set of semantic styles plus the structural colors the
// dialog overlays need for borders. Construct via Default, Greyscale, or
// Resolve.
type Theme struct {
	// Name is the resolved theme id ("default" or "greyscale").
	Name string
	// Colored reports whether the theme emits color codes. Greyscale is false.
	Colored bool

	// Text roles.
	Title     lipgloss.Style // header / banner / section titles
	Primary   lipgloss.Style // default body text
	Secondary lipgloss.Style // secondary / muted body text
	Accent    lipgloss.Style // user prefix, highlights, focus hints
	Response  lipgloss.Style // assistant response body
	Link      lipgloss.Style // URLs / commands referenced in prose
	Code      lipgloss.Style // inline code spans and fenced code blocks
	Dim       lipgloss.Style // faint footnotes ("… N more")
	UserBody  lipgloss.Style // user's own prompt text in scrollback (de-emphasized)

	// Diff roles (write_file / edit previews and results).
	DiffAdd  lipgloss.Style // added lines (+)
	DiffDel  lipgloss.Style // removed lines (-)
	DiffMeta lipgloss.Style // hunk headers and file markers (@@, ---, +++)

	// Status roles.
	Error   lipgloss.Style
	Warning lipgloss.Style
	Success lipgloss.Style

	// Selection / focus.
	Selected lipgloss.Style // highlighted list row

	// Structural colors for borders (dialog overlays, confirm band).
	BorderColor      lipgloss.TerminalColor // default panel border
	FocusBorderColor lipgloss.TerminalColor // focused / awaiting-input border

	// InputBg is the background color for the chat input row. Used by the TUI
	// to set PromptStyle/TextStyle/CursorStyle on the textinput so the typing
	// zone has a visible affordance. NoColor{} on greyscale themes.
	InputBg lipgloss.TerminalColor

	// SpinnerGradient is the ordered set of hex colors the working spinner cycles
	// through while the agent is busy (a smooth loop, last stop interpolating back
	// to the first). Empty on greyscale themes, which keep a static spinner.
	SpinnerGradient []string

	// TitleGradient is the set of hex colors used for the text gradient on
	// titles (like the exit summary) to match gemini-cli's styling. Empty on
	// greyscale themes.
	TitleGradient []string
}

// Default palette: purple-leaning dark theme. Accents lean lightly purple; the
// status colors stay conventional so errors/warnings remain recognizable.
func Default() Theme {
	const (
		purple      = "#9B7EDE" // accent / user prefix / focus
		purpleLight = "#D7AFFF" // titles / highlights
		purpleDeep  = "#6C5CE7" // selected background
		link        = "#87AFFF" // blue-ish for links/commands
		code        = "#5FD7AF" // teal for code spans/blocks
		grey        = "243"     // secondary
		greyDim     = "240"     // dim
		red         = "#FF5F87"
		yellow      = "#FFD75F"
		green       = "#5FD787"
	)
	return Theme{
		Name:             "default",
		Colored:          true,
		Title:            lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(purpleLight)),
		Primary:          lipgloss.NewStyle(),
		Secondary:        lipgloss.NewStyle().Foreground(lipgloss.Color(grey)),
		Accent:           lipgloss.NewStyle().Foreground(lipgloss.Color(purple)),
		Response:         lipgloss.NewStyle(),
		Link:             lipgloss.NewStyle().Foreground(lipgloss.Color(link)),
		Code:             lipgloss.NewStyle().Foreground(lipgloss.Color(code)),
		Dim:              lipgloss.NewStyle().Foreground(lipgloss.Color(greyDim)).Italic(true),
		UserBody:         lipgloss.NewStyle().Foreground(lipgloss.Color(grey)),
		DiffAdd:          lipgloss.NewStyle().Foreground(lipgloss.Color(green)),
		DiffDel:          lipgloss.NewStyle().Foreground(lipgloss.Color(red)),
		DiffMeta:         lipgloss.NewStyle().Foreground(lipgloss.Color(greyDim)),
		Error:            lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(red)),
		Warning:          lipgloss.NewStyle().Foreground(lipgloss.Color(yellow)),
		Success:          lipgloss.NewStyle().Foreground(lipgloss.Color(green)),
		Selected:         lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color(purpleDeep)),
		BorderColor:      lipgloss.Color(grey),
		FocusBorderColor: lipgloss.Color(purple),
		InputBg:          lipgloss.Color("235"),
		SpinnerGradient: []string{
			purple, // #9B7EDE
			link,   // #87AFFF blue
			code,   // #5FD7AF teal/cyan
			green,  // #5FD787
			yellow, // #FFD75F
			red,    // #FF5F87
		},
		TitleGradient: []string{
			"#4796E4", // blue
			"#847ACE", // purple
			"#C3677F", // off-red
		},
	}
}

// Greyscale palette: no color codes, only bold/faint/reverse attributes. Used
// for NO_COLOR and ui.theme: "greyscale". The layout matches Default so the two
// are visually interchangeable apart from chroma.
func Greyscale() Theme {
	return Theme{
		Name:             "greyscale",
		Colored:          false,
		Title:            lipgloss.NewStyle().Bold(true),
		Primary:          lipgloss.NewStyle(),
		Secondary:        lipgloss.NewStyle().Faint(true),
		Accent:           lipgloss.NewStyle().Bold(true),
		Response:         lipgloss.NewStyle(),
		Link:             lipgloss.NewStyle().Underline(true),
		Code:             lipgloss.NewStyle().Faint(true),
		Dim:              lipgloss.NewStyle().Faint(true),
		UserBody:         lipgloss.NewStyle().Faint(true),
		DiffAdd:          lipgloss.NewStyle().Bold(true),
		DiffDel:          lipgloss.NewStyle().Reverse(true),
		DiffMeta:         lipgloss.NewStyle().Faint(true),
		Error:            lipgloss.NewStyle().Bold(true),
		Warning:          lipgloss.NewStyle().Bold(true),
		Success:          lipgloss.NewStyle().Bold(true),
		Selected:         lipgloss.NewStyle().Reverse(true),
		BorderColor:      lipgloss.NoColor{},
		FocusBorderColor: lipgloss.NoColor{},
		InputBg:          lipgloss.NoColor{},
	}
}

// GradientText renders the string with a multi-stop color gradient using RGB
// linear interpolation across its runes. The supplied hex stops (e.g. "#4796E4")
// are distributed evenly across the string length. If the theme is not Colored
// (e.g. Greyscale), the string is returned unchanged or wrapped in a plain style.
func (t Theme) GradientText(text string, style lipgloss.Style, hexStops []string) string {
	if !t.Colored || len(hexStops) < 2 {
		return style.Render(text)
	}

	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return ""
	}

	// Parse hex stops into RGB.
	type rgb struct{ r, g, b float64 }
	stops := make([]rgb, 0, len(hexStops))
	for _, hex := range hexStops {
		var r, g, b uint8
		hex = strings.TrimPrefix(hex, "#")
		if len(hex) == 3 {
			fmt.Sscanf(hex, "%1x%1x%1x", &r, &g, &b)
			r, g, b = r*17, g*17, b*17
		} else if len(hex) == 6 {
			fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
		} else {
			continue // skip invalid
		}
		stops = append(stops, rgb{float64(r), float64(g), float64(b)})
	}
	if len(stops) < 2 {
		return style.Render(text)
	}

	var b strings.Builder
	segments := float64(len(stops) - 1)

	for i, r := range runes {
		// Calculate progress [0, 1] across the whole string.
		var p float64
		if n > 1 {
			p = float64(i) / float64(n-1)
		}

		// Find which segment this point falls into.
		segFloat := p * segments
		segIdx := int(segFloat)
		if segIdx >= len(stops)-1 {
			segIdx = len(stops) - 2
		}

		// Progress within the current segment.
		localP := segFloat - float64(segIdx)

		c1 := stops[segIdx]
		c2 := stops[segIdx+1]

		// Lerp.
		fr := c1.r + (c2.r-c1.r)*localP
		fg := c1.g + (c2.g-c1.g)*localP
		fb := c1.b + (c2.b-c1.b)*localP

		hex := fmt.Sprintf("#%02X%02X%02X", uint8(math.Round(fr)), uint8(math.Round(fg)), uint8(math.Round(fb)))
		b.WriteString(style.Foreground(lipgloss.Color(hex)).Render(string(r)))
	}

	return b.String()
}

// Resolve picks a theme from an optional name plus the NO_COLOR signal. NO_COLOR
// (any non-empty value, per the no-color.org convention) forces greyscale.
func Resolve(name string, noColor bool) Theme {
	if noColor {
		return Greyscale()
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "greyscale", "grayscale", "no-color", "nocolor", "none", "mono", "monochrome":
		return Greyscale()
	default:
		return Default()
	}
}
