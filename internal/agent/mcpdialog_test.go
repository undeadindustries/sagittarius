package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/ui/mcpdialog"
)

// Env is only consumed by the stdio transport (it becomes the child process
// environment). For HTTP/SSE servers it is inert, so saving an HTTP form must
// not persist it — otherwise values entered there (typically HTTP headers, in
// the wrong field) sit in settings.json looking like they do something.
func TestConfigFromFormDropsEnvForHTTPTransport(t *testing.T) {
	t.Parallel()
	form := mcpdialog.ServerForm{
		Name:      "mempalace",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Env:       "CF-Access-Client-Id=abc",
		Headers:   "CF-Access-Client-Secret=xyz",
	}
	cfg, err := configFromForm(form)
	if err != nil {
		t.Fatalf("configFromForm: %v", err)
	}
	if len(cfg.Env) != 0 {
		t.Fatalf("env must not be stored for an HTTP server (it is never sent); got %v", cfg.Env)
	}
	if cfg.Headers["CF-Access-Client-Secret"] != "xyz" {
		t.Fatalf("headers not parsed: %v", cfg.Headers)
	}
	if cfg.HTTPURL != "https://example.com/mcp" || cfg.Type != "http" {
		t.Fatalf("http transport fields wrong: %+v", cfg)
	}
}

func TestConfigFromFormKeepsEnvForStdio(t *testing.T) {
	t.Parallel()
	form := mcpdialog.ServerForm{
		Name:    "demo",
		Command: "echo",
		Env:     "A=B",
	}
	cfg, err := configFromForm(form)
	if err != nil {
		t.Fatalf("configFromForm: %v", err)
	}
	if cfg.Env["A"] != "B" {
		t.Fatalf("env not parsed for stdio: %v", cfg.Env)
	}
}

func testMCPDialogDeps(t *testing.T) (mcpdialog.Deps, *config.Documents, []string) {
	t.Helper()
	t.Cleanup(credentials.LockTestGlobals())

	stores := map[string]*memoryCredStore{}
	var mu sync.Mutex
	credentials.SetActiveBackendForTesting(func(_ context.Context, service string) credentials.Store {
		mu.Lock()
		defer mu.Unlock()
		if stores[service] == nil {
			stores[service] = &memoryCredStore{values: map[string]string{}}
		}
		return stores[service]
	})
	t.Cleanup(credentials.ResetForTesting)

	home, project := t.TempDir(), t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)
	docs, err := config.LoadDocuments(project)
	if err != nil {
		t.Fatalf("LoadDocuments: %v", err)
	}
	app := &App{docs: docs}
	return app.MCPDialogDeps(), docs, []string{home, project}
}

func TestMCPSaveServerDivertsSecretHeaders(t *testing.T) {
	deps, docs, dirs := testMCPDialogDeps(t)
	ctx := testContext(t)

	form := mcpdialog.ServerForm{
		Name:      "cf-mcp",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   "CF-Access-Client-Id=my-id,CF-Access-Client-Secret=cfast_12345,X-Custom=plain",
	}

	if err := deps.SaveServer(ctx, "", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	// Secret header must be diverted to the credentials layer.
	secretVal, err := credentials.ResolveMCPServerHeader(ctx, "cf-mcp", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader: %v", err)
	}
	if secretVal != "cfast_12345" {
		t.Fatalf("header secret in credentials store = %q, want cfast_12345", secretVal)
	}

	// Non-secret header must NOT be stored in credentials layer.
	nonSecret, err := credentials.ResolveMCPServerHeader(ctx, "cf-mcp", "CF-Access-Client-Id")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader non-secret: %v", err)
	}
	if nonSecret != "" {
		t.Fatalf("non-secret header stored in credentials: %q", nonSecret)
	}

	// In settings.json, the secret header must be replaced with ${secret:NAME}.
	servers, err := docs.Global.MCPServers()
	if err != nil {
		t.Fatalf("MCPServers: %v", err)
	}
	cfg := servers["cf-mcp"]
	if cfg.Headers["CF-Access-Client-Secret"] != "${secret:CF-Access-Client-Secret}" {
		t.Fatalf("CF-Access-Client-Secret in config = %q, want ${secret:CF-Access-Client-Secret}", cfg.Headers["CF-Access-Client-Secret"])
	}
	if cfg.Headers["CF-Access-Client-Id"] != "my-id" {
		t.Fatalf("CF-Access-Client-Id in config = %q, want my-id", cfg.Headers["CF-Access-Client-Id"])
	}
	if cfg.Headers["X-Custom"] != "plain" {
		t.Fatalf("X-Custom in config = %q, want plain", cfg.Headers["X-Custom"])
	}

	// Plaintext secret must never appear on disk.
	assertNoFileContains(t, dirs, "cfast_12345")
}

func TestMCPSaveServerRetainsExistingSecretReferencesWithoutClobbering(t *testing.T) {
	deps, _, _ := testMCPDialogDeps(t)
	ctx := testContext(t)

	// Pre-seed credentials store with the secret.
	if err := credentials.SetMCPServerHeaderSecret(ctx, "cf-mcp", "CF-Access-Client-Secret", "initial_secret"); err != nil {
		t.Fatalf("SetMCPServerHeaderSecret: %v", err)
	}

	// Form with an existing reference (as loaded when editing).
	form := mcpdialog.ServerForm{
		Name:      "cf-mcp",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   "CF-Access-Client-Secret=${secret:CF-Access-Client-Secret},CF-Access-Client-Id=my-id",
	}

	if err := deps.SaveServer(ctx, "cf-mcp", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	// Secret in store must NOT be overwritten with "${secret:...}".
	secretVal, err := credentials.ResolveMCPServerHeader(ctx, "cf-mcp", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader: %v", err)
	}
	if secretVal != "initial_secret" {
		t.Fatalf("header secret in credentials store = %q, want initial_secret", secretVal)
	}
}

func TestMCPSaveServerRemovesUnreferencedHeaderSecrets(t *testing.T) {
	deps, _, _ := testMCPDialogDeps(t)
	ctx := testContext(t)

	// First save with a secret header.
	form := mcpdialog.ServerForm{
		Name:      "cf-mcp",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   "CF-Access-Client-Secret=initial_secret",
	}
	if err := deps.SaveServer(ctx, "", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	// Second save: user removes CF-Access-Client-Secret and uses CF-Access-Token instead.
	form.Headers = "CF-Access-Token=new_token_secret"
	if err := deps.SaveServer(ctx, "cf-mcp", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer edit: %v", err)
	}

	// Old secret header must be deleted.
	oldVal, err := credentials.ResolveMCPServerHeader(ctx, "cf-mcp", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader old: %v", err)
	}
	if oldVal != "" {
		t.Fatalf("old header secret was not cleaned up: %q", oldVal)
	}

	// New secret header must exist.
	newVal, err := credentials.ResolveMCPServerHeader(ctx, "cf-mcp", "CF-Access-Token")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader new: %v", err)
	}
	if newVal != "new_token_secret" {
		t.Fatalf("new header secret = %q, want new_token_secret", newVal)
	}
}

func TestMCPSaveServerRenamesServerMigratesHeaderSecrets(t *testing.T) {
	deps, _, _ := testMCPDialogDeps(t)
	ctx := testContext(t)

	// Save original server.
	form := mcpdialog.ServerForm{
		Name:      "old-server",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   "CF-Access-Client-Secret=migrated_secret",
	}
	if err := deps.SaveServer(ctx, "", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	// Rename server.
	form.Name = "new-server"
	form.Headers = "CF-Access-Client-Secret=${secret:CF-Access-Client-Secret}"
	if err := deps.SaveServer(ctx, "old-server", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer rename: %v", err)
	}

	// New server must have the secret.
	newVal, err := credentials.ResolveMCPServerHeader(ctx, "new-server", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader new-server: %v", err)
	}
	if newVal != "migrated_secret" {
		t.Fatalf("new server secret = %q, want migrated_secret", newVal)
	}

	// Old server secret must be cleaned up.
	oldVal, err := credentials.ResolveMCPServerHeader(ctx, "old-server", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader old-server: %v", err)
	}
	if oldVal != "" {
		t.Fatalf("old server secret was not deleted: %q", oldVal)
	}
}

func TestMCPRemoveServerDeletesHeaderSecrets(t *testing.T) {
	deps, _, _ := testMCPDialogDeps(t)
	ctx := testContext(t)

	// Save server with header secret and bearer token.
	form := mcpdialog.ServerForm{
		Name:      "to-delete",
		Transport: mcpdialog.TransportHTTP,
		URL:       "https://example.com/mcp",
		Headers:   "CF-Access-Client-Secret=doomed_secret",
		Bearer:    "doomed_bearer",
	}
	if err := deps.SaveServer(ctx, "", form, config.ScopeGlobal); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}

	// Remove server.
	if err := deps.RemoveServer(ctx, "to-delete"); err != nil {
		t.Fatalf("RemoveServer: %v", err)
	}

	// Header secret must be deleted.
	headerVal, err := credentials.ResolveMCPServerHeader(ctx, "to-delete", "CF-Access-Client-Secret")
	if err != nil {
		t.Fatalf("ResolveMCPServerHeader: %v", err)
	}
	if headerVal != "" {
		t.Fatalf("header secret still present after remove: %q", headerVal)
	}

	// Bearer token must be deleted.
	bearerVal, err := credentials.ResolveMCPServerBearer(ctx, "to-delete")
	if err != nil {
		t.Fatalf("ResolveMCPServerBearer: %v", err)
	}
	if bearerVal != "" {
		t.Fatalf("bearer token still present after remove: %q", bearerVal)
	}
}
