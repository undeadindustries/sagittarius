package slash

import (
	"fmt"
	"strconv"
	"strings"
)

func copyCommand() Command {
	return Command{
		Name:        "copy",
		Description: "Copy the last assistant reply, or /copy code [N|all] for pasteable commands",
		Handler:     handleCopy,
		SubCommands: []Command{
			{
				Name:        "code",
				Description: "Copy fenced code from the last reply (default last block, or N / all)",
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
	blocks := extractFencedCodeBlocks(text)
	if len(blocks) == 0 {
		return InfoResult("No fenced code in the last reply. Use /copy for the full message.")
	}

	arg := strings.ToLower(strings.TrimSpace(ctx.Args))
	if arg == "" {
		// Default to copying the last block. Safer than joining all blocks,
		// especially when the reply includes both dry-run and destructive commands.
		return Result{Handled: true, Clipboard: blocks[len(blocks)-1]}
	}
	if arg == "all" {
		return Result{Handled: true, Clipboard: strings.Join(blocks, "\n\n")}
	}

	idx, err := strconv.Atoi(arg)
	if err != nil || idx < 1 || idx > len(blocks) {
		if len(blocks) == 1 {
			return InfoResult("Invalid block number. Only 1 block is available.")
		}
		return InfoResult(fmt.Sprintf("Invalid block number %q. Valid range is 1 to %d, or 'all'.", ctx.Args, len(blocks)))
	}
	return Result{Handled: true, Clipboard: blocks[idx-1]}
}

// extractFencedCodeBlocks returns the bodies of markdown fenced code blocks in text,
// in document order. Fence markers and language tags are stripped. Empty fences are skipped.
// An unclosed fence at EOF still yields its captured body so a truncated reply is copyable.
// extractFencedCodeBlocks returns the bodies of markdown fenced code blocks in text,
// in document order. Fence markers and language tags are stripped. Empty fences are skipped.
// An unclosed fence at EOF still yields its captured body so a truncated reply is copyable.
func extractFencedCodeBlocks(text string) []string {
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
	return blocks
}

// extractFencedCode returns the joined bodies of markdown fenced code blocks in text,
// preserved for backward compatibility and test convenience.
func extractFencedCode(text string) (string, int) {
	blocks := extractFencedCodeBlocks(text)
	return strings.Join(blocks, "\n\n"), len(blocks)
}
