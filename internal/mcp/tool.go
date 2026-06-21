package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
}

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
	desc := t.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", t.toolName, t.serverName)
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

func newDiscoveredTool(client *Client, tool *sdkmcp.Tool) *DiscoveredTool {
	return &DiscoveredTool{
		client:     client,
		tool:       tool,
		wireName:   FormatToolName(client.cfg.Name, tool.Name),
		serverName: client.cfg.Name,
		toolName:   tool.Name,
		trust:      client.cfg.Trust,
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
