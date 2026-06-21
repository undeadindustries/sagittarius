package parity_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// openAISSEChunk is a minimal OpenAI chat completions SSE delta.
type openAISSEChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Choices []deltaChoice `json:"choices"`
}

type deltaChoice struct {
	Index        int        `json:"index"`
	Delta        deltaField `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type deltaField struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// newMockOpenAIServer returns an httptest server that handles POST /v1/chat/completions
// by streaming back a short SSE response with responseText.
func newMockOpenAIServer(t *testing.T, responseText string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   []map[string]interface{}{{"id": "gpt-mock", "object": "model"}},
			})
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		stop := "stop"
		chunks := buildSSEChunks(responseText, stop)
		for _, chunk := range chunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// buildSSEChunks splits responseText into two chunks to simulate streaming.
func buildSSEChunks(text, stopReason string) []openAISSEChunk {
	// First chunk: role header.
	chunks := []openAISSEChunk{
		{
			ID:     "mock-1",
			Object: "chat.completion.chunk",
			Choices: []deltaChoice{
				{Index: 0, Delta: deltaField{Role: "assistant"}},
			},
		},
	}

	// Split text in half to emit two content chunks.
	mid := len(text) / 2
	if mid == 0 {
		mid = len(text)
	}
	part1 := text[:mid]
	part2 := text[mid:]

	chunks = append(chunks, openAISSEChunk{
		ID:     "mock-1",
		Object: "chat.completion.chunk",
		Choices: []deltaChoice{
			{Index: 0, Delta: deltaField{Content: part1}},
		},
	})
	if part2 != "" {
		chunks = append(chunks, openAISSEChunk{
			ID:     "mock-1",
			Object: "chat.completion.chunk",
			Choices: []deltaChoice{
				{Index: 0, Delta: deltaField{Content: part2}, FinishReason: &stopReason},
			},
		})
	}
	return chunks
}

// mockResponseText is the expected text that the mock server returns.
const mockResponseText = "Hello from mock server"
