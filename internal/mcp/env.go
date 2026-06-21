package mcp

import (
	"context"
	"os"
	"regexp"
	"strings"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExpandEnvVars replaces ${VAR} placeholders in s with environment values.
func ExpandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

// ResolveHeaders expands env vars and merges a stored bearer token when Authorization is absent.
func ResolveHeaders(ctx context.Context, serverName string, headers map[string]string, bearerFn func(context.Context, string) (string, error)) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		out[k] = ExpandEnvVars(v)
	}
	if _, ok := out["Authorization"]; !ok && bearerFn != nil {
		if token, err := bearerFn(ctx, serverName); err == nil && strings.TrimSpace(token) != "" {
			out["Authorization"] = "Bearer " + strings.TrimSpace(token)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
