package slash

import (
	"fmt"
	"strconv"
	"strings"
)

func diffCommand() Command {
	return Command{
		Name:        "diff",
		Description: "Show file changes made this session (optionally /diff <path> to filter)",
		Handler:     handleDiff,
	}
}

func undoCommand() Command {
	return Command{
		Name:        "undo",
		Description: "Revert the last file change (/undo <n> reverts the last n)",
		Handler:     handleUndo,
	}
}

func handleDiff(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Diff unavailable.")
	}
	out, err := ctx.Deps.Hooks.SnapshotDiff(strings.TrimSpace(ctx.Args))
	if err != nil {
		return ErrorResult(fmt.Errorf("diff: %w", err))
	}
	if strings.TrimSpace(out) == "" {
		return InfoResult("No file changes recorded this session.")
	}
	return InfoResult(out)
}

func handleUndo(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Undo unavailable.")
	}
	n := 1
	if arg := strings.TrimSpace(ctx.Args); arg != "" {
		parsed, err := strconv.Atoi(arg)
		if err != nil || parsed < 1 {
			return ErrorResult(fmt.Errorf("undo: expected a positive number, got %q", arg))
		}
		n = parsed
	}
	restored, err := ctx.Deps.Hooks.SnapshotUndo(n)
	if err != nil {
		// Surface the underlying error verbatim; some files may still have been
		// restored (reported in restored).
		if len(restored) > 0 {
			return InfoResult(
				fmt.Sprintf("Restored %d file(s): %s\n%v",
					len(restored), strings.Join(restored, ", "), err),
			)
		}
		return ErrorResult(fmt.Errorf("undo: %w", err))
	}
	if len(restored) == 0 {
		return InfoResult("Nothing to undo.")
	}
	return InfoResult(fmt.Sprintf("Reverted %d file(s): %s",
		len(restored), strings.Join(restored, ", ")))
}
