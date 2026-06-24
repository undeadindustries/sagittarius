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
