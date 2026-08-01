package toolkit

import (
	"fmt"
	"strings"
)

// RenderPlain returns a plain text, monospace table representation of the
// scan report suitable for TUI scrollback or terminal output.
func (r *Report) RenderPlain() string {
	var b strings.Builder
	b.WriteString("Host Toolkit Checklist\n")
	b.WriteString("----------------------\n")

	for _, g := range r.Groups {
		fmt.Fprintf(&b, "%s\n", g.Name)
		for _, it := range g.Items {
			icon := "[ ]"
			if it.Installed {
				icon = "[x]"
			}
			line := fmt.Sprintf("  %s %-25s", icon, it.Name)
			if !it.Installed && it.InstallHint != "" {
				line += " Hint: " + it.InstallHint
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
