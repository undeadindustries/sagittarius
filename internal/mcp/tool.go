package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// DiscoveredTool is an MCP tool ready for registration in the tool registry.
type DiscoveredTool struct {
	client     *Client
	tool       *sdkmcp.Tool
	wireName   string
	serverName string
	toolName   string
	trust      bool
	// prune is owned by the Manager and shared by every discovered tool, so a
	// live settings toggle applies without rediscovery. Nil means never prune.
	prune *atomic.Bool
}

// pruneDescriptionLimit caps an MCP tool description when pruning is on.
// Verbose servers routinely ship multi-kilobyte descriptions per tool, which
// dominate the request before the conversation even starts.
const pruneDescriptionLimit = 200

// Name returns the qualified wire tool name.
func (t *DiscoveredTool) Name() string { return t.wireName }

// RequiresConfirmation reports whether user confirmation is needed before execution.
func (t *DiscoveredTool) RequiresConfirmation() bool { return !t.trust }

// Declaration returns the provider tool schema.
func (t *DiscoveredTool) Declaration() provider.ToolDeclaration {
	params := map[string]any{"type": "object", "properties": map[string]any{}}
	if t.tool.InputSchema != nil {
		if m, ok := normalizeSchema(t.tool.InputSchema); ok {
			params = m
		}
	}
	prune := t.prune != nil && t.prune.Load()
	if prune {
		params = withoutPropertyDescriptions(params)
	}
	desc := t.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", t.toolName, t.serverName)
	}
	if prune {
		desc = truncateRunes(desc, pruneDescriptionLimit)
	}
	return provider.ToolDeclaration{
		Name:        t.wireName,
		Description: desc,
		Parameters:  params,
	}
}

// Execute calls the remote MCP tool.
func (t *DiscoveredTool) Execute(ctx context.Context, args map[string]any) (map[string]any, error) {
	result, err := t.client.CallTool(ctx, t.toolName, args)
	if err != nil {
		return map[string]any{"error": err.Error()}, nil
	}
	return formatCallResult(result), nil
}

// withoutPropertyDescriptions returns a shallow copy of the schema with each
// property's "description" removed. It copies rather than deleting in place:
// the schema map belongs to the sdkmcp.Tool that every Declaration call reads,
// so mutating it would corrupt the unpruned form permanently and race with a
// concurrent caller.
func withoutPropertyDescriptions(params map[string]any) map[string]any {
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return params
	}
	trimmed := make(map[string]any, len(props))
	for name, v := range props {
		prop, ok := v.(map[string]any)
		if !ok {
			trimmed[name] = v
			continue
		}
		clean := make(map[string]any, len(prop))
		for k, pv := range prop {
			if k == "description" {
				continue
			}
			clean[k] = pv
		}
		trimmed[name] = clean
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = v
	}
	out["properties"] = trimmed
	return out
}

// truncateRunes cuts s to at most limit runes, backing off to a rune boundary
// so a multi-byte description never ends in a broken sequence.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-3]) + "..."
}

func newDiscoveredTool(client *Client, tool *sdkmcp.Tool, prune *atomic.Bool) *DiscoveredTool {
	return &DiscoveredTool{
		client:     client,
		tool:       tool,
		wireName:   FormatToolName(client.cfg.Name, tool.Name),
		serverName: client.cfg.Name,
		toolName:   tool.Name,
		trust:      client.cfg.Trust,
		prune:      prune,
	}
}

func normalizeSchema(raw any) (map[string]any, bool) {
	switch v := raw.(type) {
	case map[string]any:
		return v, true
	default:
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, false
		}
		var out map[string]any
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, false
		}
		return out, true
	}
}

func formatCallResult(result *sdkmcp.CallToolResult) map[string]any {
	if result == nil {
		return map[string]any{"result": ""}
	}
	if result.IsError {
		return map[string]any{"error": contentToText(result.Content)}
	}
	if result.StructuredContent != nil {
		return map[string]any{"result": result.StructuredContent}
	}
	return map[string]any{"result": contentToText(result.Content)}
}

func contentToText(blocks []sdkmcp.Content) string {
	if len(blocks) == 0 {
		return ""
	}
	var parts []string
	for _, block := range blocks {
		if text, ok := block.(*sdkmcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 {
		data, _ := json.Marshal(blocks)
		return string(data)
	}
	return strings.Join(parts, "\n")
}
