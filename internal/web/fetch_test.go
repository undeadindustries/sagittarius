package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePrompt(t *testing.T) {
	prompt := "Check out https://example.com and http://test.org/path?q=1 as well as ftp://bad.com and malformed://url"
	valid, errs := ParsePrompt(prompt)
	if len(valid) != 2 {
		t.Fatalf("expected 2 valid URLs, got %d", len(valid))
	}
	if valid[0] != "https://example.com" || valid[1] != "http://test.org/path?q=1" {
		t.Errorf("unexpected valid urls: %v", valid)
	}
	if len(errs) != 2 {
		t.Errorf("expected 2 errors, got %d", len(errs))
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://EXAMPLE.com/", "https://example.com/"},
		{"http://example.com:80/path/", "http://example.com/path"},
		{"https://example.com:443/path/to/dir/", "https://example.com/path/to/dir"},
		{"https://example.com:8443/", "https://example.com:8443/"},
		{"https://example.com", "https://example.com"},
	}

	for _, tt := range tests {
		got := NormalizeURL(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeURL(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGitHubBlobToRaw(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			"https://github.com/undeadindustries/sagittarius/blob/main/README.md",
			"https://raw.githubusercontent.com/undeadindustries/sagittarius/main/README.md",
		},
		{
			"https://github.com/undeadindustries/sagittarius/tree/main/internal",
			"https://github.com/undeadindustries/sagittarius/tree/main/internal", // unchanged
		},
		{
			"https://example.com/blob/main/file.txt",
			"https://example.com/blob/main/file.txt",
		},
	}

	for _, tt := range tests {
		got := GitHubBlobToRaw(tt.input)
		if got != tt.expected {
			t.Errorf("GitHubBlobToRaw(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsPrivateHost(t *testing.T) {
	if !IsPrivateHost("localhost") {
		t.Error("localhost should be private")
	}
	if !IsPrivateHost("127.0.0.1") {
		t.Error("127.0.0.1 should be private")
	}
	if !IsPrivateHost("10.0.0.1") {
		t.Error("10.0.0.1 should be private")
	}
	if !IsPrivateHost("192.168.1.1") {
		t.Error("192.168.1.1 should be private")
	}
	if !IsPrivateHost("198.18.0.5") {
		t.Error("198.18.0.5 should be private")
	}
	// public IPs
	if IsPrivateHost("8.8.8.8") {
		t.Error("8.8.8.8 should be public")
	}
	if IsPrivateHost("example.com") {
		t.Error("example.com should be public")
	}
}

func TestHTMLToText(t *testing.T) {
	html := `<html><head><title>Title</title></head><body><h1>Hello</h1><p>This is <a href="https://example.com">a link</a>.</p><script>alert(1)</script></body></html>`
	expected := "Hello This is a link (https://example.com) ."
	got := HTMLToText([]byte(html), "")
	if got != expected {
		t.Errorf("HTMLToText() = %q; want %q", got, expected)
	}
}

func TestFetchURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	// Wait, httptest.NewServer binds to 127.0.0.1 which is private.
	// FetchURL uses safeHTTPClient which blocks 127.0.0.1.
	// So FetchURL on srv.URL should fail!
	_, err := FetchURL(context.Background(), srv.URL, 1024)
	if err == nil {
		t.Fatal("expected fetch to fail due to private IP block, but it succeeded")
	}
}
