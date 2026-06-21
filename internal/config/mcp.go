package config

import (
	"encoding/json"
	"fmt"
)

// MCPServerConfig mirrors fork MCPServerConfig for settings.json mcpServers entries.
type MCPServerConfig struct {
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	URL          string            `json:"url,omitempty"`
	HTTPURL      string            `json:"httpUrl,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	TCP          string            `json:"tcp,omitempty"`
	Type         string            `json:"type,omitempty"` // "sse" | "http"
	Timeout      *int              `json:"timeout,omitempty"`
	Trust        *bool             `json:"trust,omitempty"`
	Description  string            `json:"description,omitempty"`
	IncludeTools []string          `json:"includeTools,omitempty"`
	ExcludeTools []string          `json:"excludeTools,omitempty"`
	Disabled     *bool             `json:"disabled,omitempty"`
}

// MCPServers returns configured MCP servers from settings Raw passthrough.
func (s *Settings) MCPServers() (map[string]MCPServerConfig, error) {
	if s == nil || s.Raw == nil {
		return nil, nil
	}
	raw, ok := s.Raw["mcpServers"]
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	var servers map[string]MCPServerConfig
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, fmt.Errorf("decode mcpServers: %w", err)
	}
	return servers, nil
}
