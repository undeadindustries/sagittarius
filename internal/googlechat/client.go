package googlechat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
)

// DefaultBaseURL is the Google Chat REST API v1 endpoint.
const DefaultBaseURL = "https://chat.googleapis.com/v1"

// Client is the interface for Google Chat REST API operations.
type Client interface {
	CreateMessage(ctx context.Context, space string, msg *Message) (*Message, error)
	PatchMessage(ctx context.Context, name string, msg *Message, updateMask string) (*Message, error)
	DeleteMessage(ctx context.Context, name string) error
}

// RESTClient communicates with the Google Chat API over HTTP.
type RESTClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewRESTClient constructs a RESTClient with the given HTTP client and optional base URL.
func NewRESTClient(httpClient *http.Client, baseURL string) *RESTClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &RESTClient{
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// CreateMessage creates a new message in the specified space (e.g. "spaces/AAAA...").
func (c *RESTClient) CreateMessage(ctx context.Context, space string, msg *Message) (*Message, error) {
	if space == "" {
		return nil, fmt.Errorf("create message: space is required")
	}
	if msg == nil {
		return nil, fmt.Errorf("create message: msg is required")
	}

	space = strings.TrimPrefix(space, "/")
	targetURL := fmt.Sprintf("%s/%s/messages", c.baseURL, space)

	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse target url: %w", err)
	}

	q := u.Query()
	if msg.Thread != nil {
		if msg.Thread.Name != "" {
			q.Set("messageReplyOption", "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
		} else if msg.Thread.ThreadKey != "" {
			q.Set("threadKey", msg.Thread.ThreadKey)
			q.Set("messageReplyOption", "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
		}
	}
	u.RawQuery = q.Encode()

	bodyBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chat api create message error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var created Message
	if err := json.Unmarshal(respBody, &created); err != nil {
		return nil, fmt.Errorf("decode created message: %w", err)
	}
	return &created, nil
}

// PatchMessage updates an existing message specified by name (e.g. "spaces/AAAA/messages/BBBB").
func (c *RESTClient) PatchMessage(ctx context.Context, name string, msg *Message, updateMask string) (*Message, error) {
	if name == "" {
		return nil, fmt.Errorf("patch message: name is required")
	}
	if msg == nil {
		return nil, fmt.Errorf("patch message: msg is required")
	}

	name = strings.TrimPrefix(name, "/")
	targetURL := fmt.Sprintf("%s/%s", c.baseURL, name)

	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse target url: %w", err)
	}

	if updateMask != "" {
		q := u.Query()
		q.Set("updateMask", updateMask)
		u.RawQuery = q.Encode()
	}

	bodyBytes, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, u.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("chat api patch message error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var updated Message
	if err := json.Unmarshal(respBody, &updated); err != nil {
		return nil, fmt.Errorf("decode updated message: %w", err)
	}
	return &updated, nil
}

// DeleteMessage deletes a message specified by name.
func (c *RESTClient) DeleteMessage(ctx context.Context, name string) error {
	if name == "" {
		return fmt.Errorf("delete message: name is required")
	}

	name = strings.TrimPrefix(name, "/")
	targetURL := fmt.Sprintf("%s/%s", c.baseURL, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, targetURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("chat api delete message error (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// FakeClient is a thread-safe mock implementation of Client for testing.
type FakeClient struct {
	mu           sync.Mutex
	seq          uint64
	Messages     map[string]*Message
	Created      []*Message
	Patched      []*Message
	Deleted      []string
	CreateErr    error
	PatchErr     error
	DeleteErr    error
	OnCreateFunc func(space string, msg *Message) (*Message, error)
}

// NewFakeClient returns an empty FakeClient.
func NewFakeClient() *FakeClient {
	return &FakeClient{
		Messages: make(map[string]*Message),
	}
}

// CreateMessage stores and returns a simulated message.
func (f *FakeClient) CreateMessage(ctx context.Context, space string, msg *Message) (*Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.CreateErr != nil {
		return nil, f.CreateErr
	}
	if f.OnCreateFunc != nil {
		return f.OnCreateFunc(space, msg)
	}

	id := atomic.AddUint64(&f.seq, 1)
	name := fmt.Sprintf("%s/messages/msg-%d", space, id)

	clone := *msg
	clone.Name = name
	clone.Space = &Space{Name: space}
	if clone.Thread == nil {
		clone.Thread = &Thread{Name: fmt.Sprintf("%s/threads/thread-%d", space, id)}
	}

	f.Messages[name] = &clone
	f.Created = append(f.Created, &clone)
	return &clone, nil
}

// PatchMessage updates a message in memory.
func (f *FakeClient) PatchMessage(ctx context.Context, name string, msg *Message, updateMask string) (*Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.PatchErr != nil {
		return nil, f.PatchErr
	}

	existing, ok := f.Messages[name]
	if !ok {
		existing = &Message{Name: name}
	}

	clone := *existing
	if msg.Text != "" {
		clone.Text = msg.Text
	}
	if len(msg.CardsV2) > 0 {
		clone.CardsV2 = msg.CardsV2
	}

	f.Messages[name] = &clone
	f.Patched = append(f.Patched, &clone)
	return &clone, nil
}

// DeleteMessage marks a message as deleted in memory.
func (f *FakeClient) DeleteMessage(ctx context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.DeleteErr != nil {
		return f.DeleteErr
	}
	delete(f.Messages, name)
	f.Deleted = append(f.Deleted, name)
	return nil
}

// GetMessage retrieves a message from memory.
func (f *FakeClient) GetMessage(name string) *Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Messages[name]
}

// CreatedMessages returns a copy of created messages.
func (f *FakeClient) CreatedMessages() []*Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*Message(nil), f.Created...)
}

// DeletedMessages returns a copy of deleted message names.
func (f *FakeClient) DeletedMessages() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Deleted...)
}

// PatchedMessages returns a copy of patched messages.
func (f *FakeClient) PatchedMessages() []*Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*Message(nil), f.Patched...)
}
