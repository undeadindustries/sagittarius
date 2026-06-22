package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// SetMCPServer adds or replaces an MCP server entry in settings, persisting it
// to the Raw passthrough. The caller is responsible for flushing the mutation
// to disk via Loader.Save. Inline secrets in headers are rejected to keep
// settings.json credential-free (use ${ENV_VAR} references or the bearer store).
func (s *Settings) SetMCPServer(name string, cfg MCPServerConfig) error {
	if s == nil {
		return fmt.Errorf("set mcp server: nil settings")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("mcp server name must not be empty")
	}
	if name != strings.TrimSpace(name) || strings.ContainsAny(name, " \t\n") {
		return fmt.Errorf("mcp server name %q must not contain whitespace", name)
	}
	if err := validateNoInlineMCPSecrets(cfg); err != nil {
		return err
	}
	servers, err := s.MCPServers()
	if err != nil {
		return err
	}
	if servers == nil {
		servers = make(map[string]MCPServerConfig)
	}
	servers[name] = cfg
	return s.writeMCPServers(servers)
}

// RemoveMCPServer deletes an MCP server entry. It is a no-op when the server is
// absent. The caller flushes the mutation via Loader.Save.
func (s *Settings) RemoveMCPServer(name string) error {
	if s == nil {
		return fmt.Errorf("remove mcp server: nil settings")
	}
	servers, err := s.MCPServers()
	if err != nil {
		return err
	}
	if _, ok := servers[name]; !ok {
		return nil
	}
	delete(servers, name)
	return s.writeMCPServers(servers)
}

// SetMCPServerDisabled toggles the disabled flag on an existing server.
func (s *Settings) SetMCPServerDisabled(name string, disabled bool) error {
	if s == nil {
		return fmt.Errorf("set mcp server disabled: nil settings")
	}
	servers, err := s.MCPServers()
	if err != nil {
		return err
	}
	cfg, ok := servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}
	cfg.Disabled = &disabled
	servers[name] = cfg
	return s.writeMCPServers(servers)
}

// SetMCPServerToolFilter replaces the include/exclude tool lists for a server.
// Empty slices clear the corresponding filter.
func (s *Settings) SetMCPServerToolFilter(name string, include, exclude []string) error {
	if s == nil {
		return fmt.Errorf("set mcp server tool filter: nil settings")
	}
	servers, err := s.MCPServers()
	if err != nil {
		return err
	}
	cfg, ok := servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}
	cfg.IncludeTools = normalizeToolList(include)
	cfg.ExcludeTools = normalizeToolList(exclude)
	servers[name] = cfg
	return s.writeMCPServers(servers)
}

// writeMCPServers serializes the server map back into Raw["mcpServers"], or
// removes the key entirely when no servers remain.
func (s *Settings) writeMCPServers(servers map[string]MCPServerConfig) error {
	if s.Raw == nil {
		s.Raw = make(map[string]json.RawMessage)
	}
	if len(servers) == 0 {
		delete(s.Raw, "mcpServers")
		return nil
	}
	b, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("encode mcpServers: %w", err)
	}
	s.Raw["mcpServers"] = b
	return nil
}

// normalizeToolList trims, drops empties, sorts, and dedupes a tool-name slice,
// returning nil when the result is empty so the field is omitted from JSON.
func normalizeToolList(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// validateNoInlineMCPSecrets rejects literal credentials in a server's headers.
// Env-var references (${VAR}) are allowed; empty values are ignored.
func validateNoInlineMCPSecrets(cfg MCPServerConfig) error {
	for key, value := range cfg.Headers {
		if isInlineSecretHeader(key, value) {
			return fmt.Errorf("header %q must not contain an inline secret; use an env-var reference like ${TOKEN} or store a bearer token via the credentials store", key)
		}
	}
	return nil
}

func isInlineSecretHeader(key, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "${") {
		return false
	}
	lk := strings.ToLower(key)
	return lk == "authorization" ||
		strings.Contains(lk, "token") ||
		strings.Contains(lk, "key") ||
		strings.Contains(lk, "secret")
}
