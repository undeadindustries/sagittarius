package mcp

import "strings"

const (
	// QualifiedNameSeparator joins server and tool segments in wire names.
	QualifiedNameSeparator = "_"
	// ToolPrefix is the required prefix for MCP tool wire names.
	ToolPrefix = "mcp_"
)

// FormatToolName builds a qualified MCP tool name: mcp_{server}_{tool}.
func FormatToolName(serverName, toolName string) string {
	return ToolPrefix + serverName + QualifiedNameSeparator + toolName
}

// IsMCPToolName reports whether name uses the MCP qualified prefix.
func IsMCPToolName(name string) bool {
	return strings.HasPrefix(name, ToolPrefix)
}

// ParseToolName extracts server and tool from a qualified MCP tool name.
func ParseToolName(name string) (serverName, toolName string, ok bool) {
	if !IsMCPToolName(name) {
		return "", "", false
	}
	rest := strings.TrimPrefix(name, ToolPrefix)
	idx := strings.Index(rest, QualifiedNameSeparator)
	if idx <= 0 || idx >= len(rest)-1 {
		return "", "", false
	}
	return rest[:idx], rest[idx+1:], true
}
