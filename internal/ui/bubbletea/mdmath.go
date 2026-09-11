package bubbletea

import (
	"strings"
	"unicode"
)

// latexGlyphs maps a LaTeX command name (no leading backslash) to its Unicode
// stand-in. Only known commands are rewritten so Windows paths and unknown
// escapes stay literal.
var latexGlyphs = map[string]string{
	// Arrows
	"rightarrow": "→", "to": "→", "longrightarrow": "→",
	"leftarrow": "←", "gets": "←", "longleftarrow": "←",
	"Rightarrow": "⇒", "implies": "⇒", "Longrightarrow": "⇒",
	"Leftarrow": "⇐", "Longleftarrow": "⇐",
	"leftrightarrow": "↔",
	"Leftrightarrow": "⇔", "iff": "⇔",
	"uparrow": "↑", "downarrow": "↓",
	"updownarrow":    "↕",
	"mapsto":         "↦",
	"hookrightarrow": "↪", "hookleftarrow": "↩",

	// Arithmetic and comparison
	"times": "×", "cdot": "·", "div": "÷",
	"pm": "±", "mp": "∓",
	"leq": "≤", "le": "≤",
	"geq": "≥", "ge": "≥",
	"neq": "≠", "ne": "≠",
	"approx": "≈", "equiv": "≡",
	"sim": "~", "simeq": "≃", "cong": "≅",
	"ll": "≪", "gg": "≫",
	"propto": "∝",

	// Sets, logic, calculus
	"in": "∈", "notin": "∉",
	"subset": "⊂", "subseteq": "⊆",
	"supset": "⊃", "supseteq": "⊇",
	"cap": "∩", "cup": "∪",
	"forall": "∀", "exists": "∃",
	"neg": "¬", "land": "∧", "lor": "∨",
	"infty": "∞", "sqrt": "√",
	"partial": "∂", "nabla": "∇",
	"emptyset": "∅",
	"degree":   "°", "circ": "°",
	"dots": "…", "ldots": "…", "cdots": "⋯",
	"mid": "|",

	// Greek lowercase
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ",
	"epsilon": "ε", "varepsilon": "ε", "zeta": "ζ", "eta": "η",
	"theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ",
	"omicron": "ο", "pi": "π", "rho": "ρ", "varrho": "ϱ",
	"sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ",
	"phi": "φ", "varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",

	// Greek capitals
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ",
	"Xi": "Ξ", "Pi": "Π", "Sigma": "Σ", "Upsilon": "Υ",
	"Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
}

var latexTextCmds = map[string]struct{}{
	"text": {}, "mathrm": {}, "mathbf": {}, "mathit": {},
	"textrm": {}, "textbf": {}, "textit": {},
}

var latexSpaceCmds = map[string]string{
	"quad": "  ", "qquad": "    ",
	" ": " ", ",": " ", ";": " ", ":": " ", "!": "",
}

// renderMathInProse rewrites LaTeX inline math and known glyph commands into
// Unicode. Code spans, currency ($50), and unclosed shell vars ($PATH) are
// left untouched. Applied before wrap so column math sees real glyph widths.
func renderMathInProse(s string) string {
	if !strings.ContainsAny(s, `$\`) {
		return s
	}
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); {
		if n := copyCodeSpan(runes, i, &b); n > 0 {
			i += n
			continue
		}
		if inner, end, ok := scanInlineMath(runes, i); ok {
			b.WriteString(expandLatexCommands(inner))
			i = end
			continue
		}
		if glyph, end, ok := consumeLatexCommand(runes, i); ok {
			b.WriteString(glyph)
			i = end
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// copyCodeSpan writes a backtick span including its fences and returns the
// number of runes consumed. Zero means i is not a closed code span.
func copyCodeSpan(runes []rune, i int, b *strings.Builder) int {
	if i >= len(runes) || runes[i] != '`' {
		return 0
	}
	if _, end, ok := scanCodeSpan(runes, i); ok {
		b.WriteString(string(runes[i:end]))
		return end - i
	}
	return 0
}

// scanInlineMath matches $...$, $$...$$, or \(...\). A $ followed by
// space is not an opener. A $ followed by digits is math only when the
// inner span contains a command or a letter ($90^{\circ}$), not currency ($100$).
func scanInlineMath(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) {
		return "", i, false
	}
	if runes[i] == '\\' && i+1 < len(runes) && runes[i+1] == '(' {
		return scanParenMath(runes, i)
	}
	if runes[i] != '$' {
		return "", i, false
	}
	if i+1 < len(runes) && runes[i+1] == '$' {
		return scanDelimited(runes, i+2, []rune("$$"))
	}
	if !canOpenDollar(runes, i) {
		return "", i, false
	}
	return scanDollarMath(runes, i)
}

func canOpenDollar(runes []rune, i int) bool {
	if i+1 >= len(runes) {
		return false
	}
	return !unicode.IsSpace(runes[i+1])
}

func scanDollarMath(runes []rune, i int) (string, int, bool) {
	for j := i + 1; j < len(runes); j++ {
		if runes[j] == '`' {
			if _, end, ok := scanCodeSpan(runes, j); ok {
				j = end - 1
				continue
			}
		}
		if runes[j] != '$' || unicode.IsSpace(runes[j-1]) {
			continue
		}
		if j == i+1 {
			continue
		}
		inner := string(runes[i+1 : j])
		if unicode.IsDigit(runes[i+1]) && !looksLikeMathInner(inner) {
			return "", i, false
		}
		return inner, j + 1, true
	}
	return "", i, false
}

// looksLikeMathInner reports whether a $-span that starts with a digit is
// actually math ($90^{\circ}$, $2 \times 3$) rather than currency ($100$).
func looksLikeMathInner(s string) bool {
	if strings.Contains(s, `\`) {
		return true
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func scanParenMath(runes []rune, i int) (string, int, bool) {
	return scanDelimited(runes, i+2, []rune(`\)`))
}

func scanDelimited(runes []rune, start int, close []rune) (string, int, bool) {
	n := len(close)
	for j := start; j+n <= len(runes); j++ {
		if matchRunes(runes[j:], close) {
			return string(runes[start:j]), j + n, true
		}
	}
	return "", start, false
}

func matchRunes(hay, needle []rune) bool {
	if len(hay) < len(needle) {
		return false
	}
	for i, r := range needle {
		if hay[i] != r {
			return false
		}
	}
	return true
}

func expandLatexCommands(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(runes); {
		if glyph, end, ok := consumeDegree(runes, i); ok {
			b.WriteString(glyph)
			i = end
			continue
		}
		if glyph, end, ok := consumeLatexCommand(runes, i); ok {
			b.WriteString(glyph)
			i = end
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

func consumeDegree(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) || runes[i] != '^' {
		return "", i, false
	}
	if i+1 < len(runes) && runes[i+1] == '{' {
		inner, end, ok := consumeBraceGroup(runes, i+1)
		if ok && expandLatexCommands(inner) == "°" {
			return "°", end, true
		}
	}
	return "", i, false
}

func consumeLatexCommand(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) || runes[i] != '\\' {
		return "", i, false
	}
	if i+1 >= len(runes) {
		return "", i, false
	}
	if runes[i+1] == '\\' {
		return " ", i + 2, true
	}
	if space, ok := latexSpaceCmds[string(runes[i+1])]; ok && !unicode.IsLetter(runes[i+1]) {
		return space, i + 2, true
	}
	end := i + 1
	for end < len(runes) && unicode.IsLetter(runes[end]) {
		end++
	}
	if end == i+1 {
		return "", i, false
	}
	return applyLatexCommand(string(runes[i+1:end]), runes, end)
}

func applyLatexCommand(name string, runes []rune, end int) (string, int, bool) {
	if space, ok := latexSpaceCmds[name]; ok {
		return space, end, true
	}
	if _, ok := latexTextCmds[name]; ok {
		if inner, after, ok := consumeBraceGroup(runes, end); ok {
			return expandLatexCommands(inner), after, true
		}
		return "", end, true
	}
	if name == "left" || name == "right" {
		return consumeLeftRight(runes, end)
	}
	glyph, ok := latexGlyphs[name]
	if !ok {
		return "", end, false
	}
	if name == "sqrt" {
		if inner, after, ok := consumeBraceGroup(runes, end); ok {
			return glyph + expandLatexCommands(inner), after, true
		}
	}
	return glyph, end, true
}

func consumeLeftRight(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) {
		return "", i, true
	}
	switch runes[i] {
	case '.':
		return "", i + 1, true
	case '(', ')', '[', ']', '|', '{', '}':
		return string(runes[i]), i + 1, true
	}
	return "", i, true
}

func consumeBraceGroup(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) || runes[i] != '{' {
		return "", i, false
	}
	depth := 1
	for j := i + 1; j < len(runes); j++ {
		switch runes[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return string(runes[i+1 : j]), j + 1, true
			}
		}
	}
	return "", i, false
}
