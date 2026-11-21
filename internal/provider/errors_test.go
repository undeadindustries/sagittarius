package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestIsContextOverflowClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		body     string
		err      error
		wantOver bool
	}{
		{
			name:     "openai context_length_exceeded",
			status:   400,
			body:     `{"error":{"message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 130000 tokens. Please reduce the length of the messages.","type":"invalid_request_error","param":"messages","code":"context_length_exceeded"}}`,
			wantOver: true,
		},
		{
			name:     "openrouter prompt is too long",
			status:   400,
			body:     `{"error":{"message":"Provider returned error: prompt is too long: 65536 tokens > 32768 maximum context length","code":400}}`,
			wantOver: true,
		},
		{
			name:     "anthropic/vllm exceeds context window",
			status:   400,
			body:     `{"error":{"message":"request is too large for model: 45000 tokens exceeds the context window of 32768"}}`,
			wantOver: true,
		},
		{
			name:     "too many tokens error",
			status:   400,
			body:     `{"error":{"message":"too many tokens in request"}}`,
			wantOver: true,
		},
		{
			name:     "sentinel error directly",
			err:      ErrContextOverflow,
			wantOver: true,
		},
		{
			name:     "unrelated 400 mentioning context",
			status:   400,
			body:     `{"error":{"message":"Invalid context parameter in search request: unrecognized field 'context'","type":"invalid_request_error"}}`,
			wantOver: false,
		},
		{
			name:     "unrelated 400 bad request",
			status:   400,
			body:     `{"error":{"message":"invalid json payload","type":"invalid_request_error"}}`,
			wantOver: false,
		},
		{
			name:     "quota exceeded is not overflow",
			status:   429,
			body:     `{"error":{"message":"rate limit exceeded"}}`,
			wantOver: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.err != nil {
				err = tc.err
			} else {
				err = mapHTTPError(tc.status, tc.body)
			}
			got := IsContextOverflow(err)
			if got != tc.wantOver {
				t.Errorf("IsContextOverflow(%v) = %v, want %v", err, got, tc.wantOver)
			}
			if tc.wantOver && !errors.Is(err, ErrContextOverflow) {
				t.Errorf("expected error to wrap ErrContextOverflow, got: %v", err)
			}
		})
	}
}

func TestMapHTTPErrorStatusAndNeutralCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		body      string
		wantIs    error
		wantSub   string
		rejectSub string
	}{
		{
			name:    "529 overloaded",
			status:  529,
			body:    `{"error":{"type":"overloaded_error","message":"Overloaded"}}`,
			wantIs:  ErrProviderOverloaded,
			wantSub: "Overloaded",
		},
		{
			name:    "413 payload too large",
			status:  413,
			body:    `{"error":{"message":"request body exceeds limit"}}`,
			wantIs:  ErrPayloadTooLarge,
			wantSub: "exceeds limit",
		},
		{
			name:    "413 with overflow phrasing wraps context overflow",
			status:  413,
			body:    `{"error":{"message":"prompt is too long: 210000 tokens"}}`,
			wantIs:  ErrContextOverflow,
			wantSub: "prompt is too long",
		},
		{
			name:      "500 uses neutral provider copy",
			status:    500,
			body:      `{"error":{"message":"backend failure"}}`,
			wantSub:   "provider request failed (HTTP 500): backend failure",
			rejectSub: "openai",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := mapHTTPError(tc.status, tc.body)
			if tc.wantIs != nil && !errors.Is(err, tc.wantIs) {
				t.Fatalf("err = %v, want errors.Is %v", err, tc.wantIs)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantSub)
			}
			if tc.rejectSub != "" && strings.Contains(strings.ToLower(err.Error()), tc.rejectSub) {
				t.Fatalf("err = %q, must not mention %q", err.Error(), tc.rejectSub)
			}
		})
	}
}
