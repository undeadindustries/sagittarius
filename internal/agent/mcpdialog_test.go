package agent

import (
	"testing"

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
