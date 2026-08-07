package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// docVersions tracks per-URI LSP document versions, 0 meaning the URI
	// has never been opened. Guarded by mu, like diags.
	docVersions map[string]int
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
			Start struct {
				Line int `json:"line"`
			} `json:"start"`
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
		cmd:         cmd,
		in:          in,
		out:         out,
		cancel:      cancel,
		pending:     make(map[int64]chan *responseMsg),
		diags:       make(map[string][]diagnostics.Finding),
		docVersions: make(map[string]int),
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
		if closeErr := c.Close(); closeErr != nil {
			slog.Debug("lsp: close after failed initialize", "error", closeErr)
		}
		return nil, err
	}

	// Send initialized
	if err := c.notify("initialized", map[string]any{}); err != nil {
		if closeErr := c.Close(); closeErr != nil {
			slog.Debug("lsp: close after failed initialized notify", "error", closeErr)
		}
		return nil, err
	}

	return c, nil
}

// closeTimeout bounds how long Close waits for the server to exit on its own
// after a shutdown/exit handshake before force-killing the process.
const closeTimeout = 2 * time.Second

// Close asks the server to shut down cleanly (the "shutdown" request followed
// by the "exit" notification, per the LSP spec) before tearing down the
// process. Canceling the process context first — the previous behavior —
// sends SIGKILL immediately, so cmd.Wait() always reported a kill error even
// on an orderly exit. If the server doesn't exit within closeTimeout, the
// process context is canceled to force-kill it.
func (c *Client) Close() error {
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), closeTimeout)
	_, _ = c.call(shutdownCtx, "shutdown", nil) // best effort
	cancelShutdown()
	_ = c.notify("exit", nil) // best effort
	_ = c.in.Close()

	waitErr := make(chan error, 1)
	go func() { waitErr <- c.cmd.Wait() }()

	select {
	case err := <-waitErr:
		c.cancel()
		return err
	case <-time.After(closeTimeout):
		c.cancel() // force-kill: cancels the CommandContext the process was started with
		return <-waitErr
	}
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
			if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					length = n
				}
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
			ch, ok := c.pending[res.ID]
			c.mu.Unlock()
			if ok {
				// Non-blocking: the channel is buffered (size 1) so the
				// first response always lands. A duplicate/late response
				// for an id whose caller already gave up must never block
				// the read loop while holding no lock.
				select {
				case ch <- &res:
				default:
				}
			}
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

// diagnosticsWaitTimeout bounds how long Diagnostics waits for the server to
// publish results after a didOpen/didChange before returning whatever has
// arrived so far.
const diagnosticsWaitTimeout = 2 * time.Second

// Diagnostics fetches diagnostics for the given paths, waiting (up to
// diagnosticsWaitTimeout) for the server to publish them.
//
// Any stale entries for these URIs from a previous call are cleared first, so
// a second call for an already-fixed file can't instantly return last round's
// findings before the server has had a chance to republish.
func (c *Client) Diagnostics(ctx context.Context, absPaths []string) ([]diagnostics.Finding, error) {
	uris := make([]string, len(absPaths))
	for i, p := range absPaths {
		uris[i] = pathToURI(p)
	}

	c.mu.Lock()
	for _, uri := range uris {
		delete(c.diags, uri)
	}
	c.mu.Unlock()

	// Simulate opening/updating the files so the server has fresh content to
	// analyze. In a real editor we'd track live buffer content; for this CLI,
	// reading from disk (the write_file tool already flushed it) is fine.
	//
	// Exactly one state-changing notification per URI per call: didOpen the
	// first time a URI is seen, didChange (with an incrementing version) on
	// every later call. Sending both unconditionally on every call (as
	// before) made the server publish diagnostics twice per round; the
	// second, redundant publish could arrive after a later round had
	// already cleared diags[uri] for its own wait, resurrecting a stale
	// finding that the earlier round's edit had already fixed.
	for i, uri := range uris {
		b, err := os.ReadFile(absPaths[i])
		if err != nil {
			continue
		}
		text := string(b)

		c.mu.Lock()
		version := c.docVersions[uri]
		c.mu.Unlock()

		if version == 0 {
			_ = c.DidOpen(absPaths[i], text)
			version = 1
		} else {
			version++
			_ = c.DidChange(absPaths[i], text, version)
		}

		c.mu.Lock()
		c.docVersions[uri] = version
		c.mu.Unlock()
	}

	waitCtx, cancel := context.WithTimeout(ctx, diagnosticsWaitTimeout)
	defer cancel()

	// Diagnostics arrive asynchronously via publishDiagnostics notifications
	// handled by readLoop, which broadcasts diagCond. This goroutine's only
	// job is to re-broadcast when waitCtx expires so the Wait loop below
	// can't block forever if the server never publishes for one of uris (it
	// always exits promptly: waitCtx is canceled at the latest by the
	// deferred cancel() above when this function returns).
	go func() {
		<-waitCtx.Done()
		c.mu.Lock()
		c.diagCond.Broadcast()
		c.mu.Unlock()
	}()

	c.mu.Lock()
	for !c.allArrivedLocked(uris) && waitCtx.Err() == nil {
		c.diagCond.Wait()
	}
	findings := c.collectLocked(uris)
	c.mu.Unlock()

	return findings, nil
}

// allArrivedLocked reports whether every uri has a published (possibly
// empty) diagnostics entry. c.mu must be held by the caller.
func (c *Client) allArrivedLocked(uris []string) bool {
	for _, uri := range uris {
		if _, ok := c.diags[uri]; !ok {
			return false
		}
	}
	return true
}

// collectLocked gathers whatever diagnostics have arrived for uris. c.mu must
// be held by the caller.
func (c *Client) collectLocked(uris []string) []diagnostics.Finding {
	var findings []diagnostics.Finding
	for _, uri := range uris {
		findings = append(findings, c.diags[uri]...)
	}
	return findings
}
