package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigureInteractiveLoggingWritesToFile verifies the interactive log
// redirect sends slog output to ~/.sagittarius/logs/sagittarius.log instead of
// stderr, which would otherwise corrupt the Bubble Tea alt-screen.
func TestConfigureInteractiveLoggingWritesToFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	// Restore the default logger so this test does not leak its handler.
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })

	configureInteractiveLogging(false)
	slog.Error("sentinel log line", "error", "context canceled")

	logPath := filepath.Join(home, ".sagittarius", "logs", "sagittarius.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "sentinel log line") {
		t.Fatalf("log file missing sentinel line:\n%s", data)
	}
}

// TestConfigureInteractiveLoggingDebugLevel verifies the debug flag lowers the
// handler level so debug records are captured.
func TestConfigureInteractiveLoggingDebugLevel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })

	configureInteractiveLogging(true)
	slog.Debug("debug sentinel")

	logPath := filepath.Join(home, ".sagittarius", "logs", "sagittarius.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), "debug sentinel") {
		t.Fatalf("debug log not captured:\n%s", data)
	}
}

// TestOpenVerboseChatLog verifies --log-verbose opens a session-scoped
// transcript file under ~/.sagittarius/logs/, distinct from sagittarius.log,
// and that a second open for the same session appends rather than truncating
// (so --resume keeps the whole history in one file).
func TestOpenVerboseChatLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SAGITTARIUS_HOME", home)

	f, err := openVerboseChatLog("sess-123")
	if err != nil {
		t.Fatalf("openVerboseChatLog: %v", err)
	}
	if _, err := f.WriteString("first line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	logPath := filepath.Join(home, ".sagittarius", "logs", "chat-verbose-sess-123.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read verbose log: %v", err)
	}
	if !strings.Contains(string(data), "sagittarius --log-verbose started") {
		t.Fatalf("verbose log missing start banner:\n%s", data)
	}
	if !strings.Contains(string(data), "first line") {
		t.Fatalf("verbose log missing written content:\n%s", data)
	}

	// Reopening the same session must append, not truncate.
	f2, err := openVerboseChatLog("sess-123")
	if err != nil {
		t.Fatalf("second openVerboseChatLog: %v", err)
	}
	if _, err := f2.WriteString("second line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read verbose log after reopen: %v", err)
	}
	if !strings.Contains(string(data), "first line") || !strings.Contains(string(data), "second line") {
		t.Fatalf("expected both writes to survive reopen (append, not truncate):\n%s", data)
	}

	// A different session ID must not collide with the first file.
	sagittariusLog := filepath.Join(home, ".sagittarius", "logs", "sagittarius.log")
	if _, statErr := os.Stat(sagittariusLog); statErr == nil {
		t.Fatalf("openVerboseChatLog must not write to sagittarius.log")
	}
}
