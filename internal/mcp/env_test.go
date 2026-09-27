package mcp

import (
	"context"
	"errors"
	"testing"
)

func TestResolveHeaders_EnvAndBearer(t *testing.T) {
	ctx := context.Background()

	t.Setenv("TEST_MCP_ENV_KEY", "env-value-123")

	headers := map[string]string{
		"X-Custom": "${TEST_MCP_ENV_KEY}",
	}

	bearerFn := func(_ context.Context, server string) (string, error) {
		if server == "test-srv" {
			return "bearer-xyz", nil
		}
		return "", nil
	}

	headerFn := func(_ context.Context, server, header string) (string, error) {
		return "", nil
	}

	got, err := ResolveHeaders(ctx, "test-srv", headers, bearerFn, headerFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["X-Custom"] != "env-value-123" {
		t.Errorf("X-Custom = %q, want %q", got["X-Custom"], "env-value-123")
	}
	if got["Authorization"] != "Bearer bearer-xyz" {
		t.Errorf("Authorization = %q, want %q", got["Authorization"], "Bearer bearer-xyz")
	}
}

func TestResolveHeaders_SecretRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	headers := map[string]string{
		"CF-Access-Client-Id":     "client-id-123",
		"CF-Access-Client-Secret": "${secret:CF-Access-Client-Secret}",
	}

	headerFn := func(_ context.Context, server, header string) (string, error) {
		if server == "cf-srv" && header == "CF-Access-Client-Secret" {
			return "super-secret-token", nil
		}
		return "", nil
	}

	got, err := ResolveHeaders(ctx, "cf-srv", headers, nil, headerFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["CF-Access-Client-Id"] != "client-id-123" {
		t.Errorf("CF-Access-Client-Id = %q, want client-id-123", got["CF-Access-Client-Id"])
	}
	if got["CF-Access-Client-Secret"] != "super-secret-token" {
		t.Errorf("CF-Access-Client-Secret = %q, want super-secret-token", got["CF-Access-Client-Secret"])
	}
}

func TestResolveHeaders_MissingSecretError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	headers := map[string]string{
		"CF-Access-Client-Secret": "${secret:CF-Access-Client-Secret}",
	}

	headerFn := func(_ context.Context, server, header string) (string, error) {
		return "", nil // missing
	}

	_, err := ResolveHeaders(ctx, "cf-srv", headers, nil, headerFn)
	if err == nil {
		t.Fatal("expected error for missing secret reference")
	}
	// Must name server and header.
	for _, want := range []string{"cf-srv", "CF-Access-Client-Secret"} {
		if !containsSubstr(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

func TestResolveHeaders_SecretResolverError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	headers := map[string]string{
		"X-Secret": "${secret:API-Key}",
	}

	boom := errors.New("keyring timeout")
	headerFn := func(_ context.Context, server, header string) (string, error) {
		return "", boom
	}

	_, err := ResolveHeaders(ctx, "my-srv", headers, nil, headerFn)
	if err == nil {
		t.Fatal("expected error on resolver failure")
	}
	if !errors.Is(err, boom) && !containsSubstr(err.Error(), "keyring timeout") {
		t.Errorf("error %q should wrap keyring timeout", err.Error())
	}
}

func TestResolveHeaders_AuthHeaderSuppressesBearer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	headers := map[string]string{
		"Authorization": "Bearer ${secret:CustomAuth}",
	}

	headerFn := func(_ context.Context, server, header string) (string, error) {
		if header == "CustomAuth" {
			return "custom-token-123", nil
		}
		return "", nil
	}

	bearerFn := func(_ context.Context, server string) (string, error) {
		return "fallback-bearer-should-not-be-used", nil
	}

	got, err := ResolveHeaders(ctx, "test-srv", headers, bearerFn, headerFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["Authorization"] != "Bearer custom-token-123" {
		t.Errorf("Authorization = %q, want Bearer custom-token-123", got["Authorization"])
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && len(sub) > 0 && stringContains(s, sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
