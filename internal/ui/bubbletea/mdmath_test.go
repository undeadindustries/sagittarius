package bubbletea

import (
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/ui/theme"
)

func TestRenderMathInProse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dollar arrow", `$\rightarrow$`, "→"},
		{"bare to", `\to`, "→"},
		{"bare rightarrow", `\rightarrow`, "→"},
		{"paren arrow", `\(\rightarrow\)`, "→"},
		{"display arrow", `$$\rightarrow$$`, "→"},
		{"left arrow", `$\leftarrow$`, "←"},
		{"implies", `$\Rightarrow$`, "⇒"},
		{"iff", `$\iff$`, "⇔"},
		{"mapsto", `$\mapsto$`, "↦"},

		{"breadcrumb", `Navigate to Devices $\rightarrow$ U5G $\rightarrow$ Settings`, "Navigate to Devices → U5G → Settings"},
		{"unifi path", `Navigate to UniFi Devices $\rightarrow$ U5G $\rightarrow$ Settings $\rightarrow$ SIM.`, "Navigate to UniFi Devices → U5G → Settings → SIM."},
		{"math letters", `$A \rightarrow B$`, "A → B"},
		{"text wrapper", `$\text{UniFi} \rightarrow \text{U5G}$`, "UniFi → U5G"},
		{"mathrm", `$\mathrm{ID} \times 2$`, "ID × 2"},

		{"leq", `$\leq$`, "≤"},
		{"geq alias", `$\ge$`, "≥"},
		{"times", `1920 \times 1080`, "1920 × 1080"},
		{"in set", `$x \in S$`, "x ∈ S"},
		{"forall", `$\forall x$`, "∀ x"},
		{"neq", `$a \neq b$`, "a ≠ b"},
		{"infty", `$\infty$`, "∞"},
		{"sqrt arg", `$\sqrt{2}$`, "√2"},
		{"degree cmd", `$90^{\circ}$`, "90°"},
		{"circ", `$\circ$`, "°"},

		{"alpha", `$\alpha$`, "α"},
		{"Delta", `$\Delta$`, "Δ"},
		{"omega", `$\omega$`, "ω"},
		{"lambda", `$\lambda$`, "λ"},

		{"code span protected", "`$\\rightarrow$`", "`$\\rightarrow$`"},
		{"code span mixed", "use `$\\times$` not $\\times$", "use `$\\times$` not ×"},
		{"currency fifty", "costs $50 today", "costs $50 today"},
		{"currency hundred closed", "price $100$", "price $100$"},
		{"shell path", "export $PATH and $HOME", "export $PATH and $HOME"},
		{"unclosed dollar", "see $foo later", "see $foo later"},
		{"unknown command", `C:\Users\rob`, `C:\Users\rob`},
		{"empty", "", ""},
		{"plain prose", "no math here", "no math here"},

		{"spacing quad", `$A\quad B$`, "A   B"},
		{"thin space", `$A\,B$`, "A B"},
		{"left right paren", `$\left( x \right)$`, "( x )"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := renderMathInProse(tc.in); got != tc.want {
				t.Errorf("renderMathInProse(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRenderMarkdownMathGlyphs(t *testing.T) {
	t.Parallel()

	md := "# Path $\\rightarrow$ Next\n\n- Navigate to UniFi Devices $\\rightarrow$ U5G $\\rightarrow$ Settings $\\rightarrow$ SIM.\n"
	got := stripANSI(strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n"))
	if strings.Contains(got, `\rightarrow`) || strings.Contains(got, "$") {
		t.Errorf("raw LaTeX leaked into rendered markdown:\n%s", got)
	}
	if !strings.Contains(got, "Path → Next") {
		t.Errorf("heading arrow missing:\n%s", got)
	}
	if !strings.Contains(got, "UniFi Devices → U5G → Settings → SIM.") {
		t.Errorf("breadcrumb missing:\n%s", got)
	}
}

func TestRenderMarkdownMathLeavesFencedCode(t *testing.T) {
	t.Parallel()

	md := "before $\\times$\n```\n$\\rightarrow$\n```\nafter $\\leq$\n"
	got := stripANSI(strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n"))
	if !strings.Contains(got, `$\rightarrow$`) {
		t.Errorf("fenced code math was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "before ×") {
		t.Errorf("prose times missing:\n%s", got)
	}
	if !strings.Contains(got, "after ≤") {
		t.Errorf("prose leq missing:\n%s", got)
	}
}

func TestRenderMarkdownMathInTable(t *testing.T) {
	t.Parallel()

	md := "| From $\\rightarrow$ To | Size |\n|---|---|\n| A $\\times$ B | $n \\leq 4$ |\n"
	got := stripANSI(strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n"))
	if strings.Contains(got, `\rightarrow`) || strings.Contains(got, `\times`) || strings.Contains(got, `\leq`) {
		t.Errorf("raw LaTeX leaked in table:\n%s", got)
	}
	if !strings.Contains(got, "From → To") {
		t.Errorf("header arrow missing:\n%s", got)
	}
	if !strings.Contains(got, "A × B") {
		t.Errorf("cell times missing:\n%s", got)
	}
	if !strings.Contains(got, "n ≤ 4") {
		t.Errorf("cell leq missing:\n%s", got)
	}
}

func TestRenderMarkdownMathLeavesCurrency(t *testing.T) {
	t.Parallel()

	md := "Budget is $50 and the env is $PATH.\n"
	got := stripANSI(strings.Join(renderMarkdown(md, 80, theme.Greyscale()), "\n"))
	if !strings.Contains(got, "$50") {
		t.Errorf("currency was eaten:\n%s", got)
	}
	if !strings.Contains(got, "$PATH") {
		t.Errorf("shell var was eaten:\n%s", got)
	}
}
