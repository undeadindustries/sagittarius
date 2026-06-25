package provider

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
)

// postSSE issues a POST with the standard OpenAI-family streaming headers and
// returns the live response. Callers own closing resp.Body. The chat and
// responses adapters share this transport; only their SSE parsing differs.
func postSSE(ctx context.Context, client *http.Client, url, bearer string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, mapOpenAITransportError(err)
	}
	return resp, nil
}

// sendOrDone delivers sr on ch unless ctx is cancelled first. It returns false
// when the context is done (the producer goroutine should stop and return) and
// true on a successful send. Every adapter (Gemini, openai-chat, openai-responses)
// routes channel sends through this helper so a consumer that stops reading
// (cancelled turn, abandoned stream) never blocks the producer indefinitely.
func sendOrDone(ctx context.Context, ch chan<- StreamResponse, sr StreamResponse) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- sr:
		return true
	}
}
