package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockChatServer is a deterministic openai-chat endpoint with tool-call support.
// Decision rules per request:
//   - a prior tool result in the message history -> final assistant text;
//   - otherwise a "write"/"create" intent in the latest user message -> a
//     write_file tool call (fixed path e2e target);
//   - otherwise plain assistant text.
func mockChatServer(t *testing.T, model, writePath, writeContent string) *httptest.Server {
	t.Helper()
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

		var body struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		sawToolResult := false
		lastUser := ""
		for _, m := range body.Messages {
			switch m.Role {
			case "tool":
				sawToolResult = true
			case "user":
				lastUser = strings.ToLower(string(m.Content))
			}
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		wantsWrite := !sawToolResult && (strings.Contains(lastUser, "write") || strings.Contains(lastUser, "create"))
		if wantsWrite {
			args, _ := json.Marshal(map[string]string{"file_path": writePath, "content": writeContent})
			emitSSE(w, map[string]any{
				"id":     "mock-1",
				"object": "chat.completion.chunk",
				"choices": []map[string]any{{
					"index": 0,
					"delta": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{{
							"index": 0,
							"id":    "call_1",
							"type":  "function",
							"function": map[string]any{
								"name":      "write_file",
								"arguments": string(args),
							},
						}},
					},
					"finish_reason": "tool_calls",
				}},
			})
		} else {
			emitSSE(w, map[string]any{
				"id":      "mock-1",
				"object":  "chat.completion.chunk",
				"choices": []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "content": "done."}, "finish_reason": "stop"}},
			})
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func emitSSE(w http.ResponseWriter, chunk map[string]any) {
	data, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// mockHome writes an isolated home with settings.json pointing the openai
// provider at the mock server, and returns the home path.
func mockHome(t *testing.T, baseURL, model string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"providers": map[string]any{
			"active": "openai",
			"openai": map[string]any{
				"baseUrl": baseURL + "/v1",
				"model":   model,
			},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return home
}

func mockEnv(home, sessionID string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"SAGITTARIUS_HOME=" + home,
		"OPENAI_API_KEY=mock-key",
		"TERM=dumb",
		"NO_COLOR=1",
	}
	if sessionID != "" {
		env = append(env, "SAGITTARIUS_SESSION_ID="+sessionID)
	}
	return env
}

func skipUnlessMock(t *testing.T) {
	t.Helper()
	if !mockMode() {
		t.Skip("mock scenarios require SAGITTARIUS_E2E_MOCK=1")
	}
}

const mockModel = "gpt-mock"

func TestE2E_MockHeadlessRead(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "e2e.txt", "ok")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	res := invoke(t, bin, work, mockEnv(home, ""),
		"--output-format", "stream-json", "-p", "list the files here")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, `"type":"text"`) {
		t.Fatalf("stream missing text:\n%s", res.stdout)
	}
}

func TestE2E_MockHeadlessWriteYolo(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "e2e.txt", "ok")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	res := invoke(t, bin, work, mockEnv(home, ""),
		"--yolo", "--output-format", "stream-json", "-p", "write e2e.txt with content ok")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	if !strings.Contains(res.stdout, `"type":"tool_start"`) || !strings.Contains(res.stdout, `"type":"tool_result"`) {
		t.Fatalf("stream missing tool events:\n%s", res.stdout)
	}
	if _, err := os.Stat(filepath.Join(work, "e2e.txt")); err != nil {
		t.Fatalf("e2e.txt not written: %v", err)
	}
}

func TestE2E_MockAskBlocksWrite(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "blocked.txt", "x")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	res := invoke(t, bin, work, mockEnv(home, ""),
		"--mode", "ask", "--yolo", "--output-format", "stream-json",
		"-p", "write blocked.txt with content x")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "blocked.txt")); err == nil {
		t.Fatalf("ask mode allowed a write:\n%s", res.stdout)
	}
}

func TestE2E_MockSlashModeShow(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "e2e.txt", "ok")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	res := invoke(t, bin, work, mockEnv(home, ""), "--slash", "/mode show")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	if !strings.Contains(strings.ToLower(res.stdout), "mode") {
		t.Fatalf("/mode show output unexpected:\n%s", res.stdout)
	}
}

func TestE2E_MockSlashToolsList(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "e2e.txt", "ok")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()

	res := invoke(t, bin, work, mockEnv(home, ""), "--slash", "/tools list")
	if res.exitCode != 0 {
		t.Fatalf("exit=%d stderr=%s", res.exitCode, res.stderr)
	}
	for _, want := range []string{"Built-in tools", "read_file", "run_shell_command"} {
		if !strings.Contains(res.stdout, want) {
			t.Fatalf("/tools list missing %q:\n%s", want, res.stdout)
		}
	}
}

func TestE2E_MockSlashMCPReloadDiscoversTool(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "e2e.txt", "ok")
	home := mockHomeWithMCP(t, srv.URL, mockModel)
	work := t.TempDir()
	env := mockEnv(home, "")

	reload := invoke(t, bin, work, env, "--slash", "/mcp reload")
	if reload.exitCode != 0 {
		t.Fatalf("reload exit=%d stderr=%s", reload.exitCode, reload.stderr)
	}

	list := invoke(t, bin, work, env, "--slash", "/tools desc")
	if list.exitCode != 0 {
		t.Fatalf("tools exit=%d stderr=%s", list.exitCode, list.stderr)
	}
	if !strings.Contains(list.stdout, "everything") {
		t.Fatalf("/tools desc missing the configured server group:\n%s", list.stdout)
	}
}

// mockHomeWithMCP writes an isolated home whose settings point the openai
// provider at the mock server and declare one stdio MCP server. The server
// command may fail to connect in CI; the test only asserts the server is listed.
func mockHomeWithMCP(t *testing.T, baseURL, model string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".sagittarius")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	settings := map[string]any{
		"providers": map[string]any{
			"active": "openai",
			"openai": map[string]any{"baseUrl": baseURL + "/v1", "model": model},
		},
		"mcpServers": map[string]any{
			"everything": map[string]any{"command": "true"},
		},
	}
	data, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return home
}

func TestE2E_MockSlashDiffUndo(t *testing.T) {
	skipUnlessMock(t)
	bin := sagittariusBin(t)
	srv := mockChatServer(t, mockModel, "note.txt", "hello")
	home := mockHome(t, srv.URL, mockModel)
	work := t.TempDir()
	session := fmt.Sprintf("e2e-mock-%d", os.Getpid())
	env := mockEnv(home, session)

	write := invoke(t, bin, work, env,
		"--yolo", "--output-format", "stream-json", "-p", "write note.txt with content hello")
	if write.exitCode != 0 {
		t.Fatalf("write exit=%d stderr=%s", write.exitCode, write.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "note.txt")); err != nil {
		t.Fatalf("note.txt not written: %v", err)
	}

	diff := invoke(t, bin, work, env, "--slash", "/diff")
	if diff.exitCode != 0 {
		t.Fatalf("diff exit=%d stderr=%s", diff.exitCode, diff.stderr)
	}
	if !strings.Contains(diff.stdout, "note.txt") {
		t.Fatalf("/diff missing note.txt:\n%s", diff.stdout)
	}

	undo := invoke(t, bin, work, env, "--slash", "/undo")
	if undo.exitCode != 0 {
		t.Fatalf("undo exit=%d stderr=%s", undo.exitCode, undo.stderr)
	}
	if _, err := os.Stat(filepath.Join(work, "note.txt")); !os.IsNotExist(err) {
		t.Fatalf("note.txt present after /undo (stat err=%v)", err)
	}
}
