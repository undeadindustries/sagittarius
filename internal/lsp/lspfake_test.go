package lsp

// A minimal fake LSP server used by client_test.go and pool_test.go so tests
// never depend on a real language server (gopls, pyright, ...) being
// installed. The test binary re-execs itself (os.Args[0]) with
// LSP_FAKE_SERVER=1; TestMain intercepts that before the normal test harness
// runs and the process becomes a tiny JSON-RPC-over-stdio server instead.
//
// Diagnostics behavior is driven by magic substrings in the file content the
// real Client sends via didOpen/didChange, so a test controls the server's
// response purely by writing files:
//
//   - content containing "TRIGGER_DIAG" -> publish one diagnostic
//   - content containing "NEVER_RESPOND" -> never publish (exercises the
//     Diagnostics wait-timeout / goroutine-leak path)
//   - anything else -> publish an empty diagnostics list

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("LSP_FAKE_SERVER") == "1" {
		runFakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFakeServer() {
	if logPath := os.Getenv("LSP_FAKE_SERVER_LOG"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintf(f, "start %d\n", os.Getpid())
			_ = f.Close()
		}
	}

	var writeMu sync.Mutex
	write := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		writeMu.Lock()
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
		writeMu.Unlock()
	}

	reader := bufio.NewReader(os.Stdin)
	for {
		length := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					length = n
				}
			}
		}
		if length == 0 {
			continue
		}

		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		var msg struct {
			ID     *int64          `json:"id,omitempty"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		switch msg.Method {
		case "initialize":
			if msg.ID != nil {
				write(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": map[string]any{}})
			}
		case "shutdown":
			if msg.ID != nil {
				write(map[string]any{"jsonrpc": "2.0", "id": *msg.ID, "result": nil})
			}
		case "exit":
			os.Exit(0)
		case "textDocument/didOpen", "textDocument/didChange":
			handleFakeDidChange(write, msg.Params)
		}
	}
}

func handleFakeDidChange(write func(v any), params json.RawMessage) {
	var p struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}

	text := p.TextDocument.Text
	if text == "" && len(p.ContentChanges) > 0 {
		text = p.ContentChanges[len(p.ContentChanges)-1].Text
	}
	if strings.Contains(text, "NEVER_RESPOND") {
		return
	}

	var diags []map[string]any
	if strings.Contains(text, "TRIGGER_DIAG") {
		diags = []map[string]any{
			{
				"range":    map[string]any{"start": map[string]any{"line": 0}},
				"severity": 1,
				"source":   "fake",
				"message":  "boom",
			},
		}
	}

	write(map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params": map[string]any{
			"uri":         p.TextDocument.URI,
			"diagnostics": diags,
		},
	})
}
