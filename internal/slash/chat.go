package slash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
)

// chatCommand returns the /chat command tree: conversation checkpoints
// (save/resume/load/delete), a Markdown/JSON export (share), and a last-request
// debug dump to file (debug). It is independent of /resume, which is left
// untouched.
func chatCommand() Command {
	tagComplete := func(deps Deps, argPrefix string) []string {
		if deps.Hooks == nil {
			return nil
		}
		tags, err := deps.Hooks.ListCheckpoints()
		if err != nil {
			return nil
		}
		return filterByPrefix(tags, argPrefix)
	}
	return Command{
		Name:        "chat",
		Description: "Manage conversation checkpoints, export the chat, and debug the last request",
		SubCommands: []Command{
			{
				Name:        "list",
				Description: "List saved checkpoints and recent sessions",
				Handler:     handleChatList,
			},
			{
				Name:        "save",
				Description: "Save the current conversation as a named checkpoint",
				Handler:     handleChatSave,
			},
			{
				Name:        "resume",
				Description: "Restore a saved checkpoint into the live session",
				Handler:     handleChatResume,
				ArgComplete: tagComplete,
			},
			{
				Name:        "load",
				Description: "Alias for /chat resume <tag>",
				Handler:     handleChatResume,
				ArgComplete: tagComplete,
			},
			{
				Name:        "delete",
				Description: "Delete a saved checkpoint",
				Handler:     handleChatDelete,
				ArgComplete: tagComplete,
			},
			{
				Name:        "debug",
				Description: "Write the most recent provider request to a JSON file",
				Handler:     handleChatDebug,
			},
			{
				Name:        "share",
				Description: "Write the conversation to a Markdown or JSON file",
				Handler:     handleChatShare,
			},
		},
		Handler: handleChatList,
	}
}

// filterByPrefix returns the entries of values that have argPrefix as a prefix.
func filterByPrefix(values []string, argPrefix string) []string {
	if argPrefix == "" {
		return values
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.HasPrefix(v, argPrefix) {
			out = append(out, v)
		}
	}
	return out
}

// handleChatList renders saved checkpoints followed by the project session list.
func handleChatList(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	var b strings.Builder
	b.WriteString("Checkpoints:\n")
	tags, err := ctx.Deps.Hooks.ListCheckpoints()
	switch {
	case err != nil:
		fmt.Fprintf(&b, "  (error listing checkpoints: %v)\n", err)
	case len(tags) == 0:
		b.WriteString("  (none)\n")
	default:
		for _, tag := range tags {
			fmt.Fprintf(&b, "  %s\n", tag)
		}
	}

	b.WriteString("\n")
	infos, err := ctx.Deps.Hooks.ListSessions()
	if err != nil {
		fmt.Fprintf(&b, "(could not list sessions: %v)", err)
	} else {
		b.WriteString(session.FormatSessionList(infos))
	}
	return InfoResult(strings.TrimRight(b.String(), "\n"))
}

// handleChatSave saves the current conversation as a named checkpoint. A
// trailing "force" (or "--force"/"-f") token overwrites an existing checkpoint.
func handleChatSave(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	fields := strings.Fields(ctx.Args)
	if len(fields) == 0 {
		return InfoResult("Usage: /chat save <tag> [force]")
	}
	tag := fields[0]
	overwrite := false
	for _, f := range fields[1:] {
		switch strings.ToLower(f) {
		case "force", "--force", "-f":
			overwrite = true
		}
	}
	path, err := ctx.Deps.Hooks.SaveCheckpoint(tag, overwrite)
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Saved checkpoint %q → %s", tag, path))
}

// handleChatResume restores a saved checkpoint into the live session and repaints
// the restored conversation into the scrollback. Also serves /chat load.
func handleChatResume(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	tag := strings.TrimSpace(ctx.Args)
	if tag == "" {
		return InfoResult("Usage: /chat resume <tag>")
	}
	summary, history, err := ctx.Deps.Hooks.ResumeCheckpoint(ctx.Ctx, tag)
	if err != nil {
		return ErrorResult(err)
	}
	res := InfoResult(summary)
	res.ClearScrollback = true
	res.Scrollback = conversationScrollback(history)
	return res
}

// conversationScrollback maps restored history into role-tagged scrollback
// entries, skipping turns that carry no visible text (e.g. tool-only model
// turns and tool responses).
func conversationScrollback(history []provider.Message) []ScrollbackEntry {
	entries := make([]ScrollbackEntry, 0, len(history))
	for _, msg := range history {
		text := messageText(msg)
		if strings.TrimSpace(text) == "" {
			continue
		}
		role := ScrollUser
		if msg.Role == provider.RoleModel {
			role = ScrollAssistant
		}
		entries = append(entries, ScrollbackEntry{Role: role, Text: text})
	}
	return entries
}

// messageText joins the text parts of a message, dropping tool-call and
// tool-response parts which are not meaningful as restored scrollback.
func messageText(msg provider.Message) string {
	var parts []string
	for _, p := range msg.Parts {
		if p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// handleChatDelete deletes a saved checkpoint.
func handleChatDelete(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	tag := strings.TrimSpace(ctx.Args)
	if tag == "" {
		return InfoResult("Usage: /chat delete <tag>")
	}
	if err := ctx.Deps.Hooks.DeleteCheckpoint(tag); err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Deleted checkpoint %q", tag))
}

// handleChatDebug writes the most recent provider request to a JSON file in the
// working directory and reports the path.
func handleChatDebug(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	path, err := ctx.Deps.Hooks.WriteRequestDebug()
	if err != nil {
		return ErrorResult(err)
	}
	return InfoResult(fmt.Sprintf("Wrote last request to %s", path))
}

// handleChatShare writes the current conversation to a Markdown (default) or
// JSON file inside the workspace. The output path is constrained to the
// workspace root to prevent writing outside the project boundary.
func handleChatShare(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Chat commands unavailable.")
	}
	name := strings.TrimSpace(ctx.Args)
	if name == "" {
		name = fmt.Sprintf("sagittarius-conversation-%s.json", time.Now().Format("20060102-150405"))
	}

	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".md" && ext != ".json" {
		return ErrorResult(fmt.Errorf("invalid file format %q: only .md and .json are supported", ext))
	}

	history, err := ctx.Deps.Hooks.CurrentHistory()
	if err != nil {
		return ErrorResult(err)
	}
	if len(history) == 0 {
		return InfoResult("No conversation found to share.")
	}

	path, err := resolveSharePath(ctx.Deps.Hooks.WorkDir(), name)
	if err != nil {
		return ErrorResult(err)
	}

	var data []byte
	if ext == ".json" {
		data, err = json.MarshalIndent(history, "", "  ")
		if err != nil {
			return ErrorResult(fmt.Errorf("marshal conversation: %w", err))
		}
	} else {
		data = []byte(renderConversationMarkdown(history))
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return ErrorResult(fmt.Errorf("write conversation: %w", err))
	}
	return InfoResult(fmt.Sprintf("Wrote conversation to %s", path))
}

// resolveSharePath resolves name against workDir and rejects any path that
// escapes the workspace root. When workDir is empty (no runner) the cleaned
// name is returned as-is, written relative to the current directory.
func resolveSharePath(workDir, name string) (string, error) {
	if workDir == "" {
		return filepath.Clean(name), nil
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)
	rel, err := filepath.Rel(workDir, path)
	if err != nil {
		return "", fmt.Errorf("resolve share path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to write %q outside the workspace", name)
	}
	return path, nil
}

// renderConversationMarkdown renders the conversation history as Markdown,
// emitting user and assistant turns with fenced blocks for tool calls and
// their results.
func renderConversationMarkdown(history []provider.Message) string {
	var b strings.Builder
	b.WriteString("# Conversation\n\n")
	for _, msg := range history {
		switch msg.Role {
		case provider.RoleModel:
			b.WriteString("## Assistant\n\n")
		default:
			b.WriteString("## You\n\n")
		}
		for _, part := range msg.Parts {
			writeMarkdownPart(&b, part)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// writeMarkdownPart renders a single message part: text verbatim, tool calls and
// tool results as labelled fenced JSON blocks.
func writeMarkdownPart(b *strings.Builder, part provider.Part) {
	if part.Text != "" {
		b.WriteString(part.Text)
		b.WriteString("\n\n")
	}
	if part.FunctionCall != nil {
		fmt.Fprintf(b, "```tool call: %s\n%s\n```\n\n",
			part.FunctionCall.Name, compactJSON(part.FunctionCall.Args))
	}
	if part.FunctionResponse != nil {
		fmt.Fprintf(b, "```tool result: %s\n%s\n```\n\n",
			part.FunctionResponse.Name, compactJSON(part.FunctionResponse.Response))
	}
}

// compactJSON marshals v to compact JSON, falling back to a Go-syntax rendering
// when marshalling fails so the export never silently drops content.
func compactJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}
