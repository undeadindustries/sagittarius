package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/undeadindustries/sagittarius/internal/diagnostics"
)

// Client is a generic JSON-RPC over stdio LSP client.
type Client struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    io.ReadCloser
	cancel context.CancelFunc

	reqID atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan *responseMsg
	diags    map[string][]diagnostics.Finding
	diagCond *sync.Cond

	RootURI string
	Ready   bool
}

type requestMsg struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type notifyMsg struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseMsg struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int64            `json:"id"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *json.RawMessage `json:"error,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type diagParam struct {
	URI         string `json:"uri"`
	Diagnostics []struct {
		Range struct {
			Start struct{ Line int `json:"line"` } `json:"start"`
		} `json:"range"`
		Severity int    `json:"severity"`
		Source   string `json:"source"`
		Message  string `json:"message"`
	} `json:"diagnostics"`
}

// Start launches the LSP server and sends the initialize request.
func Start(ctx context.Context, rootDir, command string, args ...string) (*Client, error) {
	ctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = rootDir
	cmd.Env = os.Environ()

	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	// We discard stderr or log it if needed, some servers spam it.
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	rootURI := "file://" + filepath.ToSlash(rootDir)

	c := &Client{
		cmd:     cmd,
		in:      in,
		out:     out,
		cancel:  cancel,
		pending: make(map[int64]chan *responseMsg),
		diags:   make(map[string][]diagnostics.Finding),
		RootURI: rootURI,
	}
	c.diagCond = sync.NewCond(&c.mu)

	go c.readLoop()

	// Send initialize
	_, err = c.call(ctx, "initialize", map[string]any{
		"processId": os.Getpid(),
		"rootUri":   rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
			},
		},
	})
	if err != nil {
		c.Close()
		return nil, err
	}

	// Send initialized
	if err := c.notify("initialized", map[string]any{}); err != nil {
		c.Close()
		return nil, err
	}

	c.Ready = true
	return c, nil
}

// Close gracefully shuts down the server.
func (c *Client) Close() error {
	c.cancel()
	c.notify("exit", nil) // best effort
	_ = c.in.Close()
	return c.cmd.Wait()
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.reqID.Add(1)
	req := requestMsg{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan *responseMsg, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), string(b))
	if _, err := c.in.Write([]byte(msg)); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Error != nil {
			return nil, fmt.Errorf("lsp error: %s", string(*res.Error))
		}
		return res.Result, nil
	}
}

func (c *Client) notify(method string, params any) error {
	req := notifyMsg{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), string(b))
	_, err = c.in.Write([]byte(msg))
	return err
}

func (c *Client) readLoop() {
	reader := bufio.NewReader(c.out)
	for {
		// Read headers
		var length int
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return // EOF or closed
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "Content-Length:") {
				fmt.Sscanf(line, "Content-Length: %d", &length)
			}
		}

		if length == 0 {
			continue
		}

		// Read body
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			return
		}

		var res responseMsg
		if err := json.Unmarshal(body, &res); err != nil {
			continue
		}

		if res.ID != 0 {
			c.mu.Lock()
			if ch, ok := c.pending[res.ID]; ok {
				ch <- &res
			}
			c.mu.Unlock()
		} else if res.Method == "textDocument/publishDiagnostics" {
			var p diagParam
			if err := json.Unmarshal(res.Params, &p); err == nil {
				c.handleDiagnostics(p)
			}
		}
	}
}

func (c *Client) handleDiagnostics(p diagParam) {
	var findings []diagnostics.Finding
	for _, d := range p.Diagnostics {
		sev := diagnostics.SeverityError
		switch d.Severity {
		case 1:
			sev = diagnostics.SeverityError
		case 2:
			sev = diagnostics.SeverityWarning
		case 3, 4:
			sev = diagnostics.SeverityStyle
		}

		source := d.Source
		if source == "" {
			source = "LSP"
		}

		findings = append(findings, diagnostics.Finding{
			Tool:     source,
			Severity: sev,
			Message:  fmt.Sprintf("%s:%d %s", uriToPath(p.URI), d.Range.Start.Line+1, d.Message),
		})
	}

	c.mu.Lock()
	c.diags[p.URI] = findings
	c.diagCond.Broadcast()
	c.mu.Unlock()
}

func pathToURI(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return "file://" + filepath.ToSlash(abs)
}

func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri // fallback
	}
	return filepath.FromSlash(u.Path)
}

// DidOpen tells the server a file was opened.
func (c *Client) DidOpen(path string, text string) error {
	return c.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri":        pathToURI(path),
			"languageId": "", // server usually infers from extension
			"version":    1,
			"text":       text,
		},
	})
}

// DidChange tells the server a file was modified.
func (c *Client) DidChange(path string, text string, version int) error {
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{
			"uri":     pathToURI(path),
			"version": version,
		},
		"contentChanges": []map[string]any{
			{"text": text},
		},
	})
}

// Diagnostics fetches diagnostics for the given paths, waiting for them to arrive.
func (c *Client) Diagnostics(ctx context.Context, absPaths []string) ([]diagnostics.Finding, error) {
	// First, simulate opening/updating the files if they exist
	// In a real editor, we'd sync file content. For our CLI, reading from disk is fine
	for i, p := range absPaths {
		b, err := os.ReadFile(p)
		if err == nil {
			_ = c.DidOpen(p, string(b))
			_ = c.DidChange(p, string(b), i+2)
		}
	}

	// We need to wait for the server to publish diagnostics.
	// This is inherently racey because the server sends them asynchronously
	// when it finishes analyzing. We'll wait a small amount of time, or until
	// we receive diagnostics for all paths.
	
	// Wait up to 2 seconds for diagnostics to arrive
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var allFindings []diagnostics.Finding
	
	done := make(chan struct{})
	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		
		for {
			allArrived := true
			for _, p := range absPaths {
				uri := pathToURI(p)
				if _, ok := c.diags[uri]; !ok {
					allArrived = false
					break
				}
			}
			
			if allArrived {
				break
			}
			c.diagCond.Wait()
		}
		
		for _, p := range absPaths {
			uri := pathToURI(p)
			allFindings = append(allFindings, c.diags[uri]...)
		}
		close(done)
	}()

	select {
	case <-waitCtx.Done():
		// Timeout - return what we have
		c.mu.Lock()
		for _, p := range absPaths {
			uri := pathToURI(p)
			allFindings = append(allFindings, c.diags[uri]...)
		}
		c.mu.Unlock()
		return allFindings, nil
	case <-done:
		return allFindings, nil
	}
}