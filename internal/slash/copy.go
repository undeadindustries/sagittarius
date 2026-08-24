package slash

import "strings"

func copyCommand() Command {
	return Command{
		Name:        "copy",
		Description: "Copy the last assistant reply, or /copy code for pasteable commands",
		Handler:     handleCopy,
		SubCommands: []Command{
			{
				Name:        "code",
				Description: "Copy fenced code from the last reply (no bars, no markdown fences)",
				Handler:     handleCopyCode,
			},
		},
	}
}

// handleCopy copies the most recent assistant response to the clipboard. The
// actual copy is performed by the UI layer via Result.Clipboard so the slash
// layer stays free of terminal I/O.
func handleCopy(ctx *Context) Result {
	return copyLastAssistant(ctx, false)
}

func handleCopyCode(ctx *Context) Result {
	return copyLastAssistant(ctx, true)
}

func copyLastAssistant(ctx *Context, codeOnly bool) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Clipboard unavailable.")
	}
	text := ctx.Deps.Hooks.LastAssistantText()
	if text == "" {
		return InfoResult("No assistant response to copy yet.")
	}
	if !codeOnly {
		return Result{Handled: true, Clipboard: text}
	}
	body, n := extractFencedCode(text)
	if n == 0 {
		return InfoResult("No fenced code in the last reply. Use /copy for the full message.")
	}
	return Result{Handled: true, Clipboard: body}
}

// extractFencedCode returns the bodies of markdown fenced code blocks in text,
// in document order. Fence markers and language tags are stripped. Multiple
// blocks are joined with a blank line. Empty fences are skipped. An unclosed
// fence at EOF still yields its captured body so a truncated reply is copyable.
func extractFencedCode(text string) (string, int) {
	var blocks []string
	var cur []string
	in := false
	for _, raw := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(raw), "```") {
			if in {
				if body := strings.Join(cur, "\n"); strings.TrimSpace(body) != "" {
					blocks = append(blocks, body)
				}
				cur = nil
			}
			in = !in
			continue
		}
		if in {
			cur = append(cur, strings.TrimRight(raw, "\r"))
		}
	}
	if in {
		if body := strings.Join(cur, "\n"); strings.TrimSpace(body) != "" {
			blocks = append(blocks, body)
		}
	}
	return strings.Join(blocks, "\n\n"), len(blocks)
}
