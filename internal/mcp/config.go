package mcp

import (
	"time"

	"github.com/undeadindustries/sagittarius/internal/config"
)

const defaultTimeout = 10 * time.Minute

// ServerConfig is the runtime view of an MCP server entry.
type ServerConfig struct {
	Name         string
	Command      string
	Args         []string
	Env          map[string]string
	Cwd          string
	URL          string
	HTTPURL      string
	Headers      map[string]string
	TCP          string
	Type         string
	Timeout      time.Duration
	Trust        bool
	Description  string
	IncludeTools []string
	ExcludeTools []string
	Disabled     bool
}

// FromSettings converts a config.MCPServerConfig into a runtime ServerConfig.
func FromSettings(name string, cfg config.MCPServerConfig) ServerConfig {
	timeout := defaultTimeout
	if cfg.Timeout != nil && *cfg.Timeout > 0 {
		timeout = time.Duration(*cfg.Timeout) * time.Millisecond
	}
	trust := false
	if cfg.Trust != nil {
		trust = *cfg.Trust
	}
	disabled := false
	if cfg.Disabled != nil {
		disabled = *cfg.Disabled
	}
	return ServerConfig{
		Name:         name,
		Command:      cfg.Command,
		Args:         append([]string(nil), cfg.Args...),
		Env:          copyStringMap(cfg.Env),
		Cwd:          cfg.Cwd,
		URL:          cfg.URL,
		HTTPURL:      cfg.HTTPURL,
		Headers:      copyStringMap(cfg.Headers),
		TCP:          cfg.TCP,
		Type:         cfg.Type,
		Timeout:      timeout,
		Trust:        trust,
		Description:  cfg.Description,
		IncludeTools: append([]string(nil), cfg.IncludeTools...),
		ExcludeTools: append([]string(nil), cfg.ExcludeTools...),
		Disabled:     disabled,
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
