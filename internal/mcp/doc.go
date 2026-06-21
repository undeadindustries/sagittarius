// Package mcp implements an MCP client, manager, and tool wrappers for Sagittarius.
//
// Transports: stdio (CommandTransport), Streamable HTTP, and SSE via the official
// github.com/modelcontextprotocol/go-sdk. OAuth flows are deferred; bearer tokens
// use internal/credentials (env → keychain → encrypted file).
package mcp
