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

func TestSetMCPServerRoundTrip(t *testing.T) {
	t.Parallel()
	s := &Settings{Raw: map[string]json.RawMessage{}}
	if err := s.SetMCPServer("demo", MCPServerConfig{Command: "echo", Args: []string{"hi"}}); err != nil {
		t.Fatalf("SetMCPServer() error = %v", err)
	}
	servers, err := s.MCPServers()
	if err != nil {
		t.Fatalf("MCPServers() error = %v", err)
	}
	if got := servers["demo"].Command; got != "echo" {
		t.Fatalf("command = %q, want echo", got)
	}

	// Replace existing entry.
	if err := s.SetMCPServer("demo", MCPServerConfig{URL: "http://127.0.0.1:9000", Type: "http"}); err != nil {
		t.Fatalf("SetMCPServer() replace error = %v", err)
	}
	servers, _ = s.MCPServers()
	if got := servers["demo"].URL; got != "http://127.0.0.1:9000" {
		t.Fatalf("url = %q after replace", got)
	}
	if servers["demo"].Command != "" {
		t.Fatalf("command should be cleared after replace, got %q", servers["demo"].Command)
	}
}

func TestSetMCPServerRejectsEmptyName(t *testing.T) {
	t.Parallel()
	s := &Settings{Raw: map[string]json.RawMessage{}}
	if err := s.SetMCPServer("  ", MCPServerConfig{Command: "echo"}); err == nil {
		t.Fatal("expected error for empty server name")
	}
}

func TestSetMCPServerRejectsInlineSecret(t *testing.T) {
	t.Parallel()
	s := &Settings{Raw: map[string]json.RawMessage{}}
	cfg := MCPServerConfig{
		URL:     "http://127.0.0.1:9000",
		Type:    "http",
		Headers: map[string]string{"Authorization": "Bearer sk-live-123"},
	}
	if err := s.SetMCPServer("remote", cfg); err == nil {
		t.Fatal("expected inline-secret rejection")
	}

	// Env-var reference is allowed.
	cfg.Headers["Authorization"] = "Bearer ${MCP_TOKEN}"
	if err := s.SetMCPServer("remote", cfg); err != nil {
		t.Fatalf("env-var header should be allowed, got %v", err)
	}
}

func TestRemoveMCPServer(t *testing.T) {
	t.Parallel()
	s := &Settings{Raw: map[string]json.RawMessage{}}
	if err := s.SetMCPServer("a", MCPServerConfig{Command: "a"}); err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := s.SetMCPServer("b", MCPServerConfig{Command: "b"}); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	if err := s.RemoveMCPServer("a"); err != nil {
		t.Fatalf("RemoveMCPServer() error = %v", err)
	}
	servers, _ := s.MCPServers()
	if _, ok := servers["a"]; ok {
		t.Fatal("server a should be removed")
	}
	if _, ok := servers["b"]; !ok {
		t.Fatal("server b should remain")
	}

	// Removing the last server drops the key entirely.
	if err := s.RemoveMCPServer("b"); err != nil {
		t.Fatalf("RemoveMCPServer(b) error = %v", err)
	}
	if _, ok := s.Raw["mcpServers"]; ok {
		t.Fatal("mcpServers key should be removed when empty")
	}

	// Removing an absent server is a no-op.
	if err := s.RemoveMCPServer("missing"); err != nil {
		t.Fatalf("RemoveMCPServer(missing) should be no-op, got %v", err)
	}
}

func TestSetMCPServerDisabledAndToolFilter(t *testing.T) {
	t.Parallel()
	s := &Settings{Raw: map[string]json.RawMessage{}}
	if err := s.SetMCPServer("demo", MCPServerConfig{Command: "echo"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.SetMCPServerDisabled("demo", true); err != nil {
		t.Fatalf("SetMCPServerDisabled() error = %v", err)
	}
	servers, _ := s.MCPServers()
	if servers["demo"].Disabled == nil || !*servers["demo"].Disabled {
		t.Fatal("server should be disabled")
	}

	if err := s.SetMCPServerToolFilter("demo", nil, []string{"danger", "danger", " keep "}); err != nil {
		t.Fatalf("SetMCPServerToolFilter() error = %v", err)
	}
	servers, _ = s.MCPServers()
	got := servers["demo"].ExcludeTools
	want := []string{"danger", "keep"}
	if len(got) != len(want) {
		t.Fatalf("exclude = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("exclude[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if err := s.SetMCPServerDisabled("missing", true); err == nil {
		t.Fatal("expected error for missing server")
	}
}
