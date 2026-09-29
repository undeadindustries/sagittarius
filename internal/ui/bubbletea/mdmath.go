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

	// Big operators / Calculus
	"sum": "∑", "prod": "∏", "coprod": "∐",
	"int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",

	// Logic, relations, sets
	"wedge": "∧", "vee": "∨",
	"ni": "∋", "owns": "∋",
	"nexists":  "∄",
	"setminus": "∖", "backslash": "∖",
	"therefore": "∴", "because": "∵",
	"sqsubset": "⊏", "sqsubseteq": "⊑",
	"sqsupset": "⊐", "sqsupseteq": "⊒",

	// Arithmetic, operators, geometry
	"ast": "∗", "star": "⋆", "bullet": "•",
	"oplus": "⊕", "ominus": "⊖", "otimes": "⊗", "oslash": "⊘", "odot": "⊙",
	"perp": "⊥", "parallel": "∥", "angle": "∠",
	"top": "⊤", "bot": "⊥",
	"vdash": "⊢", "dashv": "⊣", "models": "⊨",
	"prec": "≺", "succ": "≻", "preceq": "⪯", "succeq": "⪰",
	"asymp": "≍",

	// Delimiters
	"langle": "⟨", "rangle": "⟩",
	"lceil": "⌈", "rceil": "⌉",
	"lfloor": "⌊", "rfloor": "⌋",
	"Vert": "‖", "vert": "|",

	// Misc symbols
	"hbar": "ℏ", "ell": "ℓ", "aleph": "ℵ",
	"Re": "ℜ", "Im": "ℑ",
	"prime": "′",
	"dag":   "†", "ddag": "‡",

	// Directional arrows
	"nearrow": "↗", "searrow": "↘", "swarrow": "↙", "nwarrow": "↖",
}

var latexTextCmds = map[string]struct{}{
	"text": {}, "mathrm": {}, "mathbf": {}, "mathit": {},
	"textrm": {}, "textbf": {}, "textit": {}, "textnormal": {},
	"mathsf": {}, "mathtt": {}, "boldsymbol": {}, "bm": {},
	"mathcal": {}, "mathscr": {}, "mathfrak": {},
	"overline": {}, "underline": {}, "widehat": {}, "widetilde": {},
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

// scanInlineMath matches $...$, $$...$$, \(...\), or \[...\]. A $ followed by
// space is not an opener. A $ followed by digits is math only when the
// inner span contains a command or a letter ($90^{\circ}$), not currency ($100$).
func scanInlineMath(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) {
		return "", i, false
	}
	if runes[i] == '\\' && i+1 < len(runes) {
		if runes[i+1] == '(' {
			return scanParenMath(runes, i)
		}
		if runes[i+1] == '[' {
			return scanBracketMath(runes, i)
		}
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

func scanBracketMath(runes []rune, i int) (string, int, bool) {
	return scanDelimited(runes, i+2, []rune(`\]`))
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
		if glyph, end, ok := consumeScript(runes, i); ok {
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

var superscriptMap = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
	'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '=': '⁼', '(': '⁽', ')': '⁾',
	'n': 'ⁿ', 'i': 'ⁱ',
}

var subscriptMap = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄',
	'5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '=': '₌', '(': '₍', ')': '₎',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ',
	'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ',
	'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ',
	'v': 'ᵥ', 'x': 'ₓ',
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
	if i+1 < len(runes) && runes[i+1] == '\\' {
		end := i + 2
		for end < len(runes) && unicode.IsLetter(runes[end]) {
			end++
		}
		cmd := string(runes[i+1 : end])
		if cmd == "circ" || cmd == "degree" {
			return "°", end, true
		}
	}
	return "", i, false
}

func consumeScript(runes []rune, i int) (string, int, bool) {
	if i >= len(runes) {
		return "", i, false
	}
	op := runes[i]
	if op != '^' && op != '_' {
		return "", i, false
	}
	if op == '^' {
		if glyph, end, ok := consumeDegree(runes, i); ok {
			return glyph, end, true
		}
	}
	var charMap map[rune]rune
	if op == '^' {
		charMap = superscriptMap
	} else {
		charMap = subscriptMap
	}
	arg, nextI, ok := consumeMathArg(runes, i+1)
	if !ok || arg == "" {
		return "", i, false
	}
	expandedArg := expandLatexCommands(arg)
	argRunes := []rune(expandedArg)
	var b strings.Builder
	allMapped := true
	for _, r := range argRunes {
		if mapped, ok := charMap[r]; ok {
			b.WriteRune(mapped)
		} else {
			allMapped = false
			break
		}
	}
	if allMapped && b.Len() > 0 {
		return b.String(), nextI, true
	}
	if len(argRunes) == 1 {
		return string(op) + expandedArg, nextI, true
	}
	return string(op) + "(" + expandedArg + ")", nextI, true
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
	if runes[i+1] == '{' || runes[i+1] == '}' {
		return string(runes[i+1]), i + 2, true
	}
	if runes[i+1] == '|' {
		return "‖", i + 2, true
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
	if isFractionCmd(name) {
		return consumeFraction(runes, end)
	}
	if isBinomialCmd(name) {
		return consumeBinomial(runes, end)
	}
	if name == "sqrt" {
		return consumeSqrt(runes, end)
	}
	if name == "mathbb" {
		return consumeMathbb(runes, end)
	}
	if _, ok := latexTextCmds[name]; ok {
		for end < len(runes) && unicode.IsSpace(runes[end]) {
			end++
		}
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
	return glyph, end, true
}

var vulgarFractions = map[[2]string]string{
	{"1", "2"}:  "½",
	{"1", "3"}:  "⅓",
	{"2", "3"}:  "⅔",
	{"1", "4"}:  "¼",
	{"3", "4"}:  "¾",
	{"1", "5"}:  "⅕",
	{"2", "5"}:  "⅖",
	{"3", "5"}:  "⅗",
	{"4", "5"}:  "⅘",
	{"1", "6"}:  "⅙",
	{"5", "6"}:  "⅚",
	{"1", "7"}:  "⅐",
	{"1", "8"}:  "⅛",
	{"3", "8"}:  "⅜",
	{"5", "8"}:  "⅝",
	{"7", "8"}:  "⅞",
	{"1", "9"}:  "⅑",
	{"1", "10"}: "⅒",
}

func isFractionCmd(name string) bool {
	switch name {
	case "frac", "dfrac", "tfrac", "cfrac", "nicefrac", "sfrac":
		return true
	}
	return false
}

func consumeFraction(runes []rune, i int) (string, int, bool) {
	num, nextI, ok := consumeMathArg(runes, i)
	if !ok {
		return "", i, false
	}
	den, afterDen, ok := consumeMathArg(runes, nextI)
	if !ok {
		return "", i, false
	}
	expandedNum := strings.TrimSpace(expandLatexCommands(num))
	expandedDen := strings.TrimSpace(expandLatexCommands(den))
	return formatFraction(expandedNum, expandedDen), afterDen, true
}

func formatFraction(num, den string) string {
	cleanNum := strings.TrimSpace(num)
	cleanDen := strings.TrimSpace(den)

	if vf, ok := vulgarFractions[[2]string{cleanNum, cleanDen}]; ok {
		return vf
	}
	if strings.HasPrefix(cleanNum, "-") {
		posNum := strings.TrimSpace(cleanNum[1:])
		if vf, ok := vulgarFractions[[2]string{posNum, cleanDen}]; ok {
			return "-" + vf
		}
	}
	if strings.HasPrefix(cleanDen, "-") {
		posDen := strings.TrimSpace(cleanDen[1:])
		if vf, ok := vulgarFractions[[2]string{cleanNum, posDen}]; ok {
			return "-" + vf
		}
	}

	n := cleanNum
	d := cleanDen

	if needsNumeratorParens(n) {
		n = "(" + n + ")"
	}
	if needsDenominatorParens(d) {
		d = "(" + d + ")"
	}
	return n + "/" + d
}

func isEnclosedInParens(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	open := s[0]
	closeChar := byte(0)
	switch open {
	case '(':
		closeChar = ')'
	case '[':
		closeChar = ']'
	default:
		return false
	}
	if s[len(s)-1] != closeChar {
		return false
	}
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case open:
			depth++
		case closeChar:
			depth--
			if depth == 0 && i < len(s)-1 {
				return false
			}
		}
	}
	return depth == 0
}

func hasBinaryOp(s string) bool {
	trimmed := strings.TrimSpace(s)
	trimmed = strings.TrimPrefix(trimmed, "-")
	trimmed = strings.TrimPrefix(trimmed, "+")
	trimmed = strings.TrimPrefix(trimmed, "−")
	trimmed = strings.TrimSpace(trimmed)
	for _, r := range trimmed {
		switch r {
		case '+', '-', '−', '±', '∓', '*', '/', '÷', '×', '·', '=', '≠', '≈', '≡', '≤', '≥', '<', '>':
			return true
		}
	}
	return false
}

func needsNumeratorParens(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || isEnclosedInParens(s) {
		return false
	}
	if hasBinaryOp(s) || strings.Contains(s, " ") {
		return true
	}
	return false
}

func needsDenominatorParens(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || isEnclosedInParens(s) {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "−") || hasBinaryOp(s) || strings.Contains(s, " ") {
		return true
	}
	return false
}

func isBinomialCmd(name string) bool {
	switch name {
	case "binom", "dbinom", "tbinom":
		return true
	}
	return false
}

func consumeBinomial(runes []rune, i int) (string, int, bool) {
	n, nextI, ok := consumeMathArg(runes, i)
	if !ok {
		return "", i, false
	}
	k, afterK, ok := consumeMathArg(runes, nextI)
	if !ok {
		return "", i, false
	}
	expandedN := strings.TrimSpace(expandLatexCommands(n))
	expandedK := strings.TrimSpace(expandLatexCommands(k))
	return "C(" + expandedN + ", " + expandedK + ")", afterK, true
}

func consumeSqrt(runes []rune, end int) (string, int, bool) {
	i := end
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	rootIndex := ""
	if i < len(runes) && runes[i] == '[' {
		closeIdx := -1
		for j := i + 1; j < len(runes); j++ {
			if runes[j] == ']' {
				closeIdx = j
				break
			}
		}
		if closeIdx > 0 {
			rootIndex = string(runes[i+1 : closeIdx])
			i = closeIdx + 1
			for i < len(runes) && unicode.IsSpace(runes[i]) {
				i++
			}
		}
	}
	inner, after, ok := consumeMathArg(runes, i)
	if !ok {
		return "√", end, true
	}
	expanded := strings.TrimSpace(expandLatexCommands(inner))
	if hasBinaryOp(expanded) && !isEnclosedInParens(expanded) {
		expanded = "(" + expanded + ")"
	}
	glyph := "√"
	if rootIndex == "3" {
		glyph = "∛"
	} else if rootIndex == "4" {
		glyph = "∜"
	} else if rootIndex != "" {
		expandedIndex := strings.TrimSpace(expandLatexCommands(rootIndex))
		glyph = expandedIndex + "√"
	}
	return glyph + expanded, after, true
}

var blackboardBold = map[rune]rune{
	'C': 'ℂ', 'H': 'ℍ', 'N': 'ℕ', 'P': 'ℙ',
	'Q': 'ℚ', 'R': 'ℝ', 'Z': 'ℤ',
}

func consumeMathbb(runes []rune, end int) (string, int, bool) {
	for end < len(runes) && unicode.IsSpace(runes[end]) {
		end++
	}
	inner, after, ok := consumeBraceGroup(runes, end)
	if !ok {
		return "", end, true
	}
	trimmed := strings.TrimSpace(inner)
	r := []rune(trimmed)
	if len(r) == 1 {
		if bb, ok := blackboardBold[r[0]]; ok {
			return string(bb), after, true
		}
	}
	return expandLatexCommands(inner), after, true
}

func consumeMathArg(runes []rune, i int) (string, int, bool) {
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	if i >= len(runes) {
		return "", i, false
	}
	if runes[i] == '{' {
		return consumeBraceGroup(runes, i)
	}
	if runes[i] == '\\' {
		if glyph, after, ok := consumeLatexCommand(runes, i); ok {
			return glyph, after, true
		}
	}
	return string(runes[i]), i + 1, true
}

func consumeLeftRight(runes []rune, i int) (string, int, bool) {
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	if i >= len(runes) {
		return "", i, true
	}
	switch runes[i] {
	case '.':
		return "", i + 1, true
	case '(', ')', '[', ']', '|':
		return string(runes[i]), i + 1, true
	}
	if runes[i] == '\\' && i+1 < len(runes) {
		if runes[i+1] == '{' || runes[i+1] == '}' {
			return string(runes[i+1]), i + 2, true
		}
		if runes[i+1] == '|' {
			return "‖", i + 2, true
		}
		end := i + 1
		for end < len(runes) && unicode.IsLetter(runes[end]) {
			end++
		}
		if end > i+1 {
			cmd := string(runes[i+1 : end])
			if glyph, ok := latexGlyphs[cmd]; ok {
				return glyph, end, true
			}
		}
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
