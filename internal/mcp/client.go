package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/credentials"
)

// Session abstracts an MCP client session for testing and production.
type Session interface {
	ListTools(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
	Close() error
}

// Connector dials an MCP server transport.
type Connector interface {
	Connect(ctx context.Context, cfg ServerConfig) (Session, error)
}

// SDKConnector uses the official go-sdk to connect to MCP servers.
type SDKConnector struct {
	ClientName    string
	ClientVersion string
}

// Connect establishes an MCP session for cfg.
func (c *SDKConnector) Connect(ctx context.Context, cfg ServerConfig) (Session, error) {
	transport, err := buildTransport(ctx, cfg)
	if err != nil {
		return nil, err
	}
	name := c.ClientName
	if name == "" {
		name = "sagittarius"
	}
	version := c.ClientVersion
	if version == "" {
		version = "dev"
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: name, Version: version}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp connect %q: %w", cfg.Name, err)
	}
	return session, nil
}

func buildTransport(ctx context.Context, cfg ServerConfig) (sdkmcp.Transport, error) {
	if cfg.Disabled {
		return nil, fmt.Errorf("server %q is disabled", cfg.Name)
	}
	if strings.TrimSpace(cfg.Command) != "" {
		return buildStdioTransport(cfg)
	}
	url := strings.TrimSpace(cfg.HTTPURL)
	if url == "" {
		url = strings.TrimSpace(cfg.URL)
	}
	if url != "" {
		return buildHTTPTransport(ctx, cfg, url)
	}
	if strings.TrimSpace(cfg.TCP) != "" {
		return nil, fmt.Errorf("server %q: tcp transport not supported in v1", cfg.Name)
	}
	return nil, fmt.Errorf("server %q: no transport configured (need command or url/httpUrl)", cfg.Name)
}

func buildStdioTransport(cfg ServerConfig) (sdkmcp.Transport, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), flattenEnv(cfg.Env)...)
	}
	return &sdkmcp.CommandTransport{Command: cmd}, nil
}

func buildHTTPTransport(ctx context.Context, cfg ServerConfig, url string) (sdkmcp.Transport, error) {
	url = ExpandEnvVars(url)
	headers := ResolveHeaders(ctx, cfg.Name, cfg.Headers, credentials.ResolveMCPServerBearer)
	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: headerRoundTripper{base: http.DefaultTransport, headers: headers},
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Type)) {
	case "sse":
		return &sdkmcp.SSEClientTransport{Endpoint: url, HTTPClient: client}, nil
	case "http", "":
		if strings.EqualFold(cfg.Type, "") && strings.Contains(strings.ToLower(url), "/sse") {
			return &sdkmcp.SSEClientTransport{Endpoint: url, HTTPClient: client}, nil
		}
		return &sdkmcp.StreamableClientTransport{Endpoint: url, HTTPClient: client}, nil
	default:
		return &sdkmcp.StreamableClientTransport{Endpoint: url, HTTPClient: client}, nil
	}
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return base.RoundTrip(req)
}

func flattenEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+ExpandEnvVars(v))
	}
	return out
}

// ServerStatus tracks connection state for an MCP server.
type ServerStatus string

const (
	ServerDisconnected ServerStatus = "disconnected"
	ServerConnecting   ServerStatus = "connecting"
	ServerConnected    ServerStatus = "connected"
	ServerDisabled     ServerStatus = "disabled"
)

// ServerState holds runtime info for one MCP server.
type ServerState struct {
	Name      string
	Status    ServerStatus
	LastError string
	ToolCount int
	Config    ServerConfig
}

// Client wraps a single MCP server session.
type Client struct {
	cfg     ServerConfig
	session Session
	status  ServerStatus
	lastErr string
}

// NewClient connects to an MCP server using connector.
func NewClient(ctx context.Context, cfg ServerConfig, connector Connector) (*Client, error) {
	if cfg.Disabled {
		return &Client{cfg: cfg, status: ServerDisabled}, nil
	}
	c := &Client{cfg: cfg, status: ServerConnecting}
	session, err := connector.Connect(ctx, cfg)
	if err != nil {
		c.status = ServerDisconnected
		c.lastErr = err.Error()
		return c, err
	}
	c.session = session
	c.status = ServerConnected
	return c, nil
}

// Close terminates the MCP session.
func (c *Client) Close() error {
	if c.session == nil {
		return nil
	}
	err := c.session.Close()
	c.session = nil
	c.status = ServerDisconnected
	return err
}

// ListTools returns tools exposed by the server after include/exclude filtering.
func (c *Client) ListTools(ctx context.Context) ([]*sdkmcp.Tool, error) {
	all, err := c.ListAllTools(ctx)
	if err != nil {
		return nil, err
	}
	return filterTools(all, c.cfg.IncludeTools, c.cfg.ExcludeTools), nil
}

// ListAllTools returns every tool the server exposes, without applying the
// include/exclude filter. The tool inventory UI uses this to show all tools and
// their enabled state.
func (c *Client) ListAllTools(ctx context.Context) ([]*sdkmcp.Tool, error) {
	if c.session == nil {
		return nil, fmt.Errorf("mcp server %q not connected", c.cfg.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	result, err := c.session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		c.lastErr = err.Error()
		return nil, fmt.Errorf("list tools %q: %w", c.cfg.Name, err)
	}
	return result.Tools, nil
}

// Config returns the server's runtime configuration.
func (c *Client) Config() ServerConfig { return c.cfg }

// CallTool invokes a tool on the server.
func (c *Client) CallTool(ctx context.Context, toolName string, args map[string]any) (*sdkmcp.CallToolResult, error) {
	if c.session == nil {
		return nil, fmt.Errorf("mcp server %q not connected", c.cfg.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	return c.session.CallTool(ctx, &sdkmcp.CallToolParams{Name: toolName, Arguments: args})
}

// State returns the current server state snapshot.
func (c *Client) State(toolCount int) ServerState {
	return ServerState{
		Name:      c.cfg.Name,
		Status:    c.status,
		LastError: c.lastErr,
		ToolCount: toolCount,
		Config:    c.cfg,
	}
}

func filterTools(tools []*sdkmcp.Tool, include, exclude []string) []*sdkmcp.Tool {
	if len(include) == 0 && len(exclude) == 0 {
		return tools
	}
	includeSet := toSet(include)
	excludeSet := toSet(exclude)
	out := make([]*sdkmcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if len(includeSet) > 0 {
			if _, ok := includeSet[tool.Name]; !ok {
				continue
			}
		}
		if _, blocked := excludeSet[tool.Name]; blocked {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}
