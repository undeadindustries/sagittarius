package credentials

import (
	"context"
	"fmt"
)

const mcpServicePrefix = "sagittarius-mcp-"

// MCPServerServiceName returns the keychain service for MCP server credentials.
func MCPServerServiceName(serverName string) string {
	return mcpServicePrefix + serverName
}

// ResolveMCPServerBearer loads a stored bearer token for an MCP server.
// Env vars in settings headers take precedence; this is the secure-store fallback.
func ResolveMCPServerBearer(ctx context.Context, serverName string) (string, error) {
	store := newMCPStore(serverName)
	val, err := store.Get(ctx, serverName)
	if err != nil {
		return "", fmt.Errorf("resolve mcp bearer for %q: %w", serverName, err)
	}
	return val, nil
}

// SetMCPServerBearer stores a bearer token for an MCP server (AD-005).
func SetMCPServerBearer(ctx context.Context, serverName, token string) error {
	store := newMCPStore(serverName)
	if err := store.Set(ctx, serverName, token); err != nil {
		return fmt.Errorf("store mcp bearer for %q: %w", serverName, err)
	}
	return nil
}

// DeleteMCPServerBearer removes a stored bearer token for an MCP server.
func DeleteMCPServerBearer(ctx context.Context, serverName string) error {
	store := newMCPStore(serverName)
	if err := store.Delete(ctx, serverName); err != nil {
		return fmt.Errorf("delete mcp bearer for %q: %w", serverName, err)
	}
	return nil
}

func mcpHeaderAccount(headerName string) string {
	return "header:" + headerName
}

// ResolveMCPServerHeader loads a stored secret for an MCP server HTTP header.
func ResolveMCPServerHeader(ctx context.Context, serverName, headerName string) (string, error) {
	store := newMCPStore(serverName)
	val, err := store.Get(ctx, mcpHeaderAccount(headerName))
	if err != nil {
		return "", fmt.Errorf("resolve mcp header %q for %q: %w", headerName, serverName, err)
	}
	return val, nil
}

// SetMCPServerHeaderSecret stores a secret for an MCP server HTTP header.
func SetMCPServerHeaderSecret(ctx context.Context, serverName, headerName, secret string) error {
	store := newMCPStore(serverName)
	if err := store.Set(ctx, mcpHeaderAccount(headerName), secret); err != nil {
		return fmt.Errorf("store mcp header %q for %q: %w", headerName, serverName, err)
	}
	return nil
}

// DeleteMCPServerHeader removes a stored secret for an MCP server HTTP header.
func DeleteMCPServerHeader(ctx context.Context, serverName, headerName string) error {
	store := newMCPStore(serverName)
	if err := store.Delete(ctx, mcpHeaderAccount(headerName)); err != nil {
		return fmt.Errorf("delete mcp header %q for %q: %w", headerName, serverName, err)
	}
	return nil
}

func newMCPStore(serverName string) Store {
	return &hybridStore{service: MCPServerServiceName(serverName)}
}
