package mcp

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	envVarPattern    = regexp.MustCompile(`\$\{([^}]+)\}`)
	secretRefPattern = regexp.MustCompile(`\$\{secret:([^}]+)\}`)
)

// ExpandEnvVars replaces ${VAR} placeholders in s with environment values.
// Placeholders with a "secret:" prefix (e.g. ${secret:NAME}) are left untouched
// for secret resolution.
func ExpandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if strings.HasPrefix(name, "secret:") {
			return match
		}
		if val, ok := os.LookupEnv(name); ok {
			return val
		}
		return match
	})
}

// ResolveHeaders expands env vars, resolves ${secret:NAME} references via
// headerFn, and merges a stored bearer token when Authorization is absent.
// If a header references a secret that is not found, an error is returned.
func ResolveHeaders(
	ctx context.Context,
	serverName string,
	headers map[string]string,
	bearerFn func(context.Context, string) (string, error),
	headerFn func(context.Context, string, string) (string, error),
) (map[string]string, error) {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		expanded := ExpandEnvVars(v)
		if secretRefPattern.MatchString(expanded) {
			var resolveErr error
			resolved := secretRefPattern.ReplaceAllStringFunc(expanded, func(m string) string {
				if resolveErr != nil {
					return m
				}
				match := secretRefPattern.FindStringSubmatch(m)
				if len(match) < 2 {
					return m
				}
				headerName := strings.TrimSpace(match[1])
				if headerFn == nil {
					resolveErr = fmt.Errorf("mcp server %q header %q references secret %q but no secret resolver configured", serverName, k, headerName)
					return m
				}
				secret, err := headerFn(ctx, serverName, headerName)
				if err != nil {
					resolveErr = fmt.Errorf("resolve mcp server %q header %q secret %q: %w", serverName, k, headerName, err)
					return m
				}
				if strings.TrimSpace(secret) == "" {
					resolveErr = fmt.Errorf("mcp server %q header %q references secret %q which is not configured in credentials store", serverName, k, headerName)
					return m
				}
				return secret
			})
			if resolveErr != nil {
				return nil, resolveErr
			}
			expanded = resolved
		}
		out[k] = expanded
	}
	hasAuth := false
	for k := range out {
		if strings.EqualFold(k, "Authorization") {
			hasAuth = true
			break
		}
	}
	if !hasAuth && bearerFn != nil {
		if token, err := bearerFn(ctx, serverName); err == nil && strings.TrimSpace(token) != "" {
			out["Authorization"] = "Bearer " + strings.TrimSpace(token)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}
