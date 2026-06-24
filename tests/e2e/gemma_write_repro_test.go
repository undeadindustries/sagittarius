package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/credentials"
	"github.com/undeadindustries/sagittarius/internal/diff"
)

const (
	gemmaWriteModel     = "google/gemma-4-31b-it"
	ejectionMarkerSnake = "<file_written path=\"js/apps/snake.js\" lines=166 tokens=1296 cached=true>"
)

// gemmaWriteReproServer is a mock openai-chat endpoint that logs every wire
// request and simulates a model that first sends a bad write_file (ejection
// marker), then retries with valid code after Sagittarius rejects it.
func gemmaWriteReproServer(t *testing.T, model, logDir string) *httptest.Server {
	t.Helper()
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("MkdirAll log dir: %v", err)
	}
	var reqNum int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": model, "object": "model"}},
			})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}

		raw, _ := io.ReadAll(r.Body)
		reqNum++
		path := filepath.Join(logDir, fmt.Sprintf("wire-request-%02d.json", reqNum))
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Errorf("write wire log %s: %v", path, err)
		}

		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(raw, &body)

		sawToolResult := false
		lastTool := ""
		for _, m := range body.Messages {
			if m.Role != "tool" {
				continue
			}
			sawToolResult = true
			lastTool = strings.ToLower(string(m.Content))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		goodJS := "class Snake {}\nwindow.Snake = Snake;\n"
		switch {
		case !sawToolResult:
			args, _ := json.Marshal(map[string]string{
				"file_path": "js/apps/snake.js",
				"content":   ejectionMarkerSnake,
			})
			emitSSE(w, toolCallChunk("call_bad", string(args)))
		case strings.Contains(lastTool, "ejection marker") ||
			strings.Contains(lastTool, "unified diff") ||
			strings.Contains(lastTool, "placeholder"):
			args, _ := json.Marshal(map[string]string{
				"file_path": "js/apps/snake.js",
				"content":   goodJS,
			})
			emitSSE(w, toolCallChunk("call_good", string(args)))
		default:
			emitSSE(w, textChunk("done."))
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func toolCallChunk(callID, args string) map[string]any {
	return map[string]any{
		"id":     "mock-gemma",
		"object": "chat.completion.chunk",
		"choices": []map[string]any{{
			"index": 0,
			"delta": map[string]any{
				"role": "assistant",
				"tool_calls": []map[string]any{{
					"index": 0,
					"id":    callID,
					"type":  "function",
					"function": map[string]any{
						"name":      "write_file",
						"arguments": args,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
	}
}

func textChunk(text string) map[string]any {
	return map[string]any{
		"id":      "mock-gemma",
		"object":  "chat.completion.chunk",
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": text}, "finish_reason": "stop"}},
	}
}

// TestE2E_MockGemmaWriteFileWireLog drives the corrupted-file fix flow against a
// mock server that logs every provider request body. It simulates a model that
// first copies a <file_written …> ejection marker into write_file (rejected by
// Sagittarius), then retries with real code.
//
// Run:
//
//	SAGITTARIUS_E2E_MOCK=1 go test -v ./tests/e2e/ -run MockGemmaWriteFileWireLog
func TestE2E_MockGemmaWriteFileWireLog(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	logDir := filepath.Join(t.TempDir(), "wire-log")
	srv := gemmaWriteReproServer(t, mockModel, logDir)
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	snake := filepath.Join(work, "js", "apps", "snake.js")
	if err := os.MkdirAll(filepath.Dir(snake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snake, []byte(ejectionMarkerSnake), 0o644); err != nil {
		t.Fatal(err)
	}

	prompt := "The file js/apps/snake.js is corrupted — it only contains a metadata tag, not real code. " +
		"Read it, then overwrite it with a complete minimal Snake class and window.Snake = Snake export. " +
		"Use write_file with the ENTIRE file body, not a diff."
	res := invoke(t, bin, work, mockEnv(home, ""),
		"--yolo", "--output-format", "stream-json", "-p", prompt)
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", res.exitCode, res.stderr, res.stdout)
	}

	got, err := os.ReadFile(snake)
	if err != nil {
		t.Fatal(err)
	}
	if diff.LooksLikeEjectionMarker(string(got)) {
		t.Fatalf("snake.js still contains ejection marker: %q", string(got))
	}
	if !strings.Contains(string(got), "class Snake") {
		t.Fatalf("snake.js missing expected code: %q", string(got))
	}

	entries, _ := os.ReadDir(logDir)
	if len(entries) < 2 {
		t.Fatalf("expected >=2 wire requests logged, got %d in %s", len(entries), logDir)
	}
	t.Logf("wire logs written to %s (%d requests)", logDir, len(entries))
	t.Logf("stream-json:\n%s", res.stdout)
}

// TestE2E_LiveGemmaWriteFileRepro runs against the real OpenRouter Gemma model
// when explicitly opted in. It seeds a corrupted snake.js, captures stream-json
// output, and records whether the file was fixed without writing an ejection
// marker back to disk.
//
// Run (uses your ~/.sagittarius credentials and makes a billable API call):
//
//	SAGITTARIUS_E2E_GEMMA_WRITE=1 go test -v ./tests/e2e/ -run LiveGemmaWriteFileRepro -timeout 3m
func TestE2E_LiveGemmaWriteFileRepro(t *testing.T) {
	if mockMode() {
		t.Skip("mock mode: live Gemma repro skipped")
	}
	if os.Getenv("SAGITTARIUS_E2E_GEMMA_WRITE") != "1" {
		t.Skip("live Gemma write repro disabled; set SAGITTARIUS_E2E_GEMMA_WRITE=1")
	}
	ctx := context.Background()
	key, err := credentials.ResolveProviderAPIKey(ctx, "openrouter")
	if err != nil || strings.TrimSpace(key) == "" {
		t.Skip("no openrouter API key available")
	}

	bin := sagittariusBin(t)
	work := t.TempDir()
	snake := filepath.Join(work, "js", "apps", "snake.js")
	if err := os.MkdirAll(filepath.Dir(snake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snake, []byte(ejectionMarkerSnake), 0o644); err != nil {
		t.Fatal(err)
	}

	session := fmt.Sprintf("gemma-write-repro-%d", os.Getpid())
	env := append(os.Environ(),
		"SAGITTARIUS_SESSION_ID="+session,
		"GEMINI_PROVIDER=openrouter",
	)

	prompt := "Fix js/apps/snake.js. It currently only contains an invalid metadata tag, not JavaScript. " +
		"Read the file first. Then call write_file with the COMPLETE file contents: a minimal Snake class " +
		"and window.Snake = Snake at the end. Do not use diff format or placeholder tags."
	res := invoke(t, bin, work, env,
		"--yolo", "-m", gemmaWriteModel,
		"--output-format", "stream-json", "-p", prompt)
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}

	streamPath := filepath.Join(work, "gemma-write-stream.jsonl")
	if err := os.WriteFile(streamPath, []byte(res.stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("stream-json saved to %s", streamPath)
	t.Logf("stream-json:\n%s", res.stdout)

	got, err := os.ReadFile(snake)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("snake.js after turn:\n%s", string(got))

	if diff.LooksLikeEjectionMarker(string(got)) {
		t.Fatalf("model wrote ejection marker to disk again")
	}
	if diff.LooksLikeUnifiedDiff(string(got)) {
		t.Fatalf("model wrote diff-shaped content to disk")
	}
}
