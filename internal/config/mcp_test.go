package config

import (
	"encoding/json"
	"testing"
)

func TestMCPServersFromRaw(t *testing.T) {
	t.Parallel()
	raw := map[string]json.RawMessage{
		"mcpServers": json.RawMessage(`{"demo":{"command":"echo","args":["hi"]}}`),
	}
	s := &Settings{Raw: raw}
	servers, err := s.MCPServers()
	if err != nil {
		t.Fatalf("MCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("len = %d, want 1", len(servers))
	}
	if servers["demo"].Command != "echo" {
		t.Fatalf("command = %q", servers["demo"].Command)
	}
}
