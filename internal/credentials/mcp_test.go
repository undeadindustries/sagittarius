package credentials

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMCPHeaderSecretRoundTrip(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)
	stores := useMemoryBackends(t)

	const (
		server = "mempalace"
		header = "CF-Access-Client-Secret"
		secret = "cfast_12345"
		bearer = "bearer-token-xyz"
	)

	// Set both bearer and a header secret on the same server.
	if err := SetMCPServerBearer(ctx, server, bearer); err != nil {
		t.Fatalf("SetMCPServerBearer() error = %v", err)
	}
	if err := SetMCPServerHeaderSecret(ctx, server, header, secret); err != nil {
		t.Fatalf("SetMCPServerHeaderSecret() error = %v", err)
	}

	// Verify both can be resolved independently.
	gotBearer, err := ResolveMCPServerBearer(ctx, server)
	if err != nil {
		t.Fatalf("ResolveMCPServerBearer() error = %v", err)
	}
	if gotBearer != bearer {
		t.Fatalf("ResolveMCPServerBearer() = %q, want %q", gotBearer, bearer)
	}

	gotSecret, err := ResolveMCPServerHeader(ctx, server, header)
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader() error = %v", err)
	}
	if gotSecret != secret {
		t.Fatalf("ResolveMCPServerHeader() = %q, want %q", gotSecret, secret)
	}

	// Verify internal storage layout: same service, different accounts.
	svc := MCPServerServiceName(server)
	s := stores[svc]
	if s == nil {
		t.Fatalf("store for %q is nil", svc)
	}
	if s.values[server] != bearer {
		t.Errorf("bearer stored under account %q = %q, want %q", server, s.values[server], bearer)
	}
	if s.values["header:"+header] != secret {
		t.Errorf("header secret stored under account %q = %q, want %q", "header:"+header, s.values["header:"+header], secret)
	}

	// Delete header secret only. Bearer must remain.
	if err := DeleteMCPServerHeader(ctx, server, header); err != nil {
		t.Fatalf("DeleteMCPServerHeader() error = %v", err)
	}
	gotSecret, err = ResolveMCPServerHeader(ctx, server, header)
	if err != nil {
		t.Fatalf("after delete ResolveMCPServerHeader() error = %v", err)
	}
	if gotSecret != "" {
		t.Fatalf("after delete ResolveMCPServerHeader() = %q, want empty", gotSecret)
	}
	gotBearer, err = ResolveMCPServerBearer(ctx, server)
	if err != nil || gotBearer != bearer {
		t.Fatalf("bearer should survive header deletion: got %q, err = %v", gotBearer, err)
	}

	// Delete bearer.
	if err := DeleteMCPServerBearer(ctx, server); err != nil {
		t.Fatalf("DeleteMCPServerBearer() error = %v", err)
	}
	gotBearer, err = ResolveMCPServerBearer(ctx, server)
	if err != nil || gotBearer != "" {
		t.Fatalf("after delete ResolveMCPServerBearer() = %q, want empty", gotBearer)
	}
}

func TestMCPHeaderSecretFileStoreRoundTrip(t *testing.T) {
	t.Cleanup(LockTestGlobals())
	ctx := testContext(t)

	tmpDir := t.TempDir()
	credPath := filepath.Join(tmpDir, "test-credentials.json")
	SetCredentialsPathForTesting(credPath)
	t.Cleanup(func() {
		SetCredentialsPathForTesting("")
		ResetForTesting()
	})

	// Force file storage backend by making keyring unavailable.
	SetActiveBackendForTesting(func(_ context.Context, service string) Store {
		store, err := sharedEncryptedFileStore(service)
		if err != nil {
			t.Fatalf("sharedEncryptedFileStore(%q): %v", service, err)
		}
		return store
	})

	const (
		server = "cloudflare-mcp"
		header = "CF-Access-Client-Secret"
		secret = "cfast_secret_value_999"
	)

	if err := SetMCPServerHeaderSecret(ctx, server, header, secret); err != nil {
		t.Fatalf("SetMCPServerHeaderSecret() error = %v", err)
	}

	got, err := ResolveMCPServerHeader(ctx, server, header)
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader() error = %v", err)
	}
	if got != secret {
		t.Fatalf("ResolveMCPServerHeader() = %q, want %q", got, secret)
	}

	if err := DeleteMCPServerHeader(ctx, server, header); err != nil {
		t.Fatalf("DeleteMCPServerHeader() error = %v", err)
	}

	got, err = ResolveMCPServerHeader(ctx, server, header)
	if err != nil {
		t.Fatalf("after delete ResolveMCPServerHeader() error = %v", err)
	}
	if got != "" {
		t.Fatalf("after delete ResolveMCPServerHeader() = %q, want empty", got)
	}
}
