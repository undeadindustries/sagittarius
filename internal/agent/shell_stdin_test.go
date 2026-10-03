package agent

import (
	"errors"
	"os"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/tools"
)

func TestRunnerWriteShellInputAndFocus(t *testing.T) {
	t.Parallel()

	r := &Runner{
		shellStdins: make(map[string]*tools.PTYStdin),
	}

	rRead, rWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rRead.Close() }()
	defer func() { _ = rWrite.Close() }()

	stdin := &tools.PTYStdin{}
	stdin.AttachForTesting(rWrite)

	callID := "test-call-1"
	unregister := r.registerShellStdin(callID, stdin)

	// Set focus
	r.SetShellFocus(callID, true)
	if !stdin.Focused() {
		t.Fatal("expected stdin to be focused")
	}

	// Write input
	if err := r.WriteShellInput(callID, []byte("hello\n")); err != nil {
		t.Fatalf("WriteShellInput: %v", err)
	}

	buf := make([]byte, 6)
	if _, err := rRead.Read(buf); err != nil {
		t.Fatalf("Read pipe: %v", err)
	}
	if string(buf) != "hello\n" {
		t.Fatalf("got %q, want %q", string(buf), "hello\n")
	}

	// Clear focus
	r.SetShellFocus(callID, false)
	if stdin.Focused() {
		t.Fatal("expected stdin to be unfocused")
	}

	// Unregister
	unregister()
	if err := r.WriteShellInput(callID, []byte("after-unreg")); !errors.Is(err, tools.ErrPTYClosed) {
		t.Fatalf("expected ErrPTYClosed after unregister, got %v", err)
	}

	// Non-existent call ID
	if err := r.WriteShellInput("no-such-call", []byte("x")); !errors.Is(err, tools.ErrPTYClosed) {
		t.Fatalf("expected ErrPTYClosed for unknown call, got %v", err)
	}
}

func TestAppShellInputWriterPassthrough(t *testing.T) {
	t.Parallel()

	r := &Runner{
		shellStdins: make(map[string]*tools.PTYStdin),
	}
	app := &App{runner: r}

	stdin := &tools.PTYStdin{}
	callID := "app-call-1"
	unregister := r.registerShellStdin(callID, stdin)
	defer unregister()

	app.SetShellFocus(callID, true)
	if !stdin.Focused() {
		t.Fatal("expected stdin focused via app")
	}

	app.SetShellFocus(callID, false)
	if stdin.Focused() {
		t.Fatal("expected stdin unfocused via app")
	}
}
