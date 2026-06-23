package slash_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/slash"
)

func TestInitRegistered(t *testing.T) {
	t.Parallel()
	p := slash.NewProcessor()
	help := p.Registry().RenderHelp()
	if !strings.Contains(help, "/init") {
		t.Fatalf("help missing /init\n%s", help)
	}
}

func TestInitCreatesFileAndSubmitsPrompt(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	dir := t.TempDir()
	hooks.workDir = dir

	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/init", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.SubmitPrompt == "" {
		t.Fatal("expected non-empty SubmitPrompt")
	}
	if !strings.Contains(res.SubmitPrompt, "AGENTS.md") {
		t.Fatalf("SubmitPrompt missing AGENTS.md reference:\n%s", res.SubmitPrompt)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "Analyzing") {
		t.Fatalf("messages missing analysis notice: %q", joined)
	}

	info, err := os.Stat(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("stat AGENTS.md: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("expected empty AGENTS.md, got %d bytes", info.Size())
	}
}

func TestInitExistingFileNoChange(t *testing.T) {
	t.Parallel()
	deps, _, hooks := testDeps(t, nil)
	dir := t.TempDir()
	hooks.workDir = dir
	target := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(target, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	p := slash.NewProcessor()
	res := p.Process(context.Background(), "/init", deps)
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.SubmitPrompt != "" {
		t.Fatalf("expected no SubmitPrompt for existing file, got %q", res.SubmitPrompt)
	}
	joined := strings.Join(res.Messages, "\n")
	if !strings.Contains(joined, "already exists") {
		t.Fatalf("messages missing already-exists notice: %q", joined)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(data) != "keep me" {
		t.Fatalf("AGENTS.md content changed: %q", data)
	}
}
