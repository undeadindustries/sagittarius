package mcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// latencyConnector injects a per-server connect delay and optional connect
// failures so tests can observe fan-out concurrency and per-server isolation.
type latencyConnector struct {
	defaultDelay time.Duration
	delays       map[string]time.Duration
	failOn       map[string]bool
	active       int32 // current in-flight Connect calls
	peak         int32 // high-water mark of concurrent Connect calls
}

func (c *latencyConnector) Connect(ctx context.Context, cfg ServerConfig) (Session, error) {
	n := atomic.AddInt32(&c.active, 1)
	for {
		p := atomic.LoadInt32(&c.peak)
		if n <= p || atomic.CompareAndSwapInt32(&c.peak, p, n) {
			break
		}
	}
	defer atomic.AddInt32(&c.active, -1)

	delay := c.defaultDelay
	if d, ok := c.delays[cfg.Name]; ok {
		delay = d
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if c.failOn[cfg.Name] {
		return nil, fmt.Errorf("connect failed for %q", cfg.Name)
	}
	return &mockSession{tools: []*sdkmcp.Tool{{Name: "echo"}}}, nil
}

func mockServers(n int) map[string]config.MCPServerConfig {
	servers := make(map[string]config.MCPServerConfig, n)
	for i := 0; i < n; i++ {
		servers[fmt.Sprintf("srv%02d", i)] = config.MCPServerConfig{Command: "mock"}
	}
	return servers
}

// TestReloadConnectsConcurrently asserts the connect/list fan-out runs in
// parallel: reloading N servers each with the same connect latency completes in
// ~latency, not ~N*latency. A serial Reload would fail this.
func TestReloadConnectsConcurrently(t *testing.T) {
	t.Parallel()

	const n = 5
	const latency = 100 * time.Millisecond
	conn := &latencyConnector{defaultDelay: latency}
	m := NewManager(ManagerConfig{Connector: conn})

	start := time.Now()
	if err := m.Reload(context.Background(), mockServers(n)); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	elapsed := time.Since(start)

	// Serial would be ~n*latency (500ms); concurrent ~latency (100ms). The
	// threshold sits well below the serial floor to stay robust under -race.
	if elapsed >= (n-1)*latency {
		t.Fatalf("Reload took %v; expected concurrent (~%v), not serial (~%v)", elapsed, latency, n*latency)
	}
	if got := atomic.LoadInt32(&conn.peak); got < 2 {
		t.Fatalf("peak concurrent connects = %d, want >= 2 (fan-out)", got)
	}
	if got := len(m.States()); got != n {
		t.Fatalf("States() len = %d, want %d", got, n)
	}
}

// TestReloadDeterministicOrder asserts States() is name-sorted regardless of the
// order in which concurrent connections complete. Latencies are assigned inverse
// to name order so completion order is the reverse of the published order.
func TestReloadDeterministicOrder(t *testing.T) {
	t.Parallel()

	conn := &latencyConnector{delays: map[string]time.Duration{
		"srv00": 80 * time.Millisecond,
		"srv01": 60 * time.Millisecond,
		"srv02": 40 * time.Millisecond,
		"srv03": 20 * time.Millisecond,
		"srv04": 0,
	}}
	m := NewManager(ManagerConfig{Connector: conn})

	if err := m.Reload(context.Background(), mockServers(5)); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	states := m.States()
	want := []string{"srv00", "srv01", "srv02", "srv03", "srv04"}
	if len(states) != len(want) {
		t.Fatalf("States() len = %d, want %d", len(states), len(want))
	}
	for i, st := range states {
		if st.Name != want[i] {
			t.Fatalf("States()[%d].Name = %q, want %q (must be name-sorted)", i, st.Name, want[i])
		}
	}
}

// TestReloadFailingServerDoesNotBlockSiblings asserts one server's connect
// failure neither aborts nor delays the others: the healthy servers still come
// up connected and the failing one is recorded as disconnected.
func TestReloadFailingServerDoesNotBlockSiblings(t *testing.T) {
	t.Parallel()

	conn := &latencyConnector{
		defaultDelay: 20 * time.Millisecond,
		failOn:       map[string]bool{"srv02": true},
	}
	m := NewManager(ManagerConfig{Connector: conn})

	if err := m.Reload(context.Background(), mockServers(5)); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}

	states := m.States()
	if len(states) != 5 {
		t.Fatalf("States() len = %d, want 5", len(states))
	}
	connected := 0
	for _, st := range states {
		switch st.Name {
		case "srv02":
			if st.Status != ServerDisconnected {
				t.Fatalf("srv02 status = %q, want disconnected", st.Status)
			}
			if st.LastError == "" {
				t.Fatal("srv02 should record a connect error")
			}
		default:
			if st.Status != ServerConnected {
				t.Fatalf("%s status = %q, want connected", st.Name, st.Status)
			}
			connected++
		}
	}
	if connected != 4 {
		t.Fatalf("connected servers = %d, want 4", connected)
	}
	// The four healthy servers each expose one tool through the filtered path.
	if got := len(m.Tools()); got != 4 {
		t.Fatalf("Tools() len = %d, want 4", got)
	}
}

// TestApplyToolFiltersNoReconnect asserts a tool-filter toggle re-derives the
// active set from the unfiltered cache without dialing the connector again.
func TestApplyToolFiltersNoReconnect(t *testing.T) {
	t.Parallel()

	conn := &countingConnector{}
	m := NewManager(ManagerConfig{Connector: conn})

	servers := map[string]config.MCPServerConfig{
		"demo": {Command: "mock"},
	}
	if err := m.Reload(context.Background(), servers); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if got := len(m.Tools()); got != 2 {
		t.Fatalf("Tools() len = %d, want 2", got)
	}
	connectsAfterReload := atomic.LoadInt32(&conn.connects)

	// Exclude one tool: the active set shrinks, no reconnect happens.
	m.ApplyToolFilters(map[string]config.MCPServerConfig{
		"demo": {Command: "mock", ExcludeTools: []string{"danger"}},
	})
	if got := len(m.Tools()); got != 1 {
		t.Fatalf("after exclude: Tools() len = %d, want 1", got)
	}

	// Re-enable from cache: the previously excluded tool returns without a
	// reconnect, proving the unfiltered cache (not a network round trip) backs
	// the toggle.
	m.ApplyToolFilters(servers)
	if got := len(m.Tools()); got != 2 {
		t.Fatalf("after re-enable: Tools() len = %d, want 2", got)
	}
	if got := atomic.LoadInt32(&conn.connects); got != connectsAfterReload {
		t.Fatalf("connector dialed %d more times during filter toggles; want 0", got-connectsAfterReload)
	}
}

// TestReloadVsToolInventoryNoUseAfterClose is the regression for the MCP client
// use-after-close race: Reload tears down and replaces the clients (closing each
// old session) while ToolInventory/States read those same *Client pointers
// off-lock. Before the fix, Client.session was read by ListAllTools concurrently
// with Close() niling it. Must be clean under -race.
func TestReloadVsToolInventoryNoUseAfterClose(t *testing.T) {
	conn := &latencyConnector{defaultDelay: time.Millisecond}
	m := NewManager(ManagerConfig{Connector: conn})
	servers := mockServers(4)
	if err := m.Reload(context.Background(), servers); err != nil {
		t.Fatalf("initial Reload() error = %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Readers: ToolInventory copies clients under the lock then calls
	// ListAllTools off-lock; States reads each client's status/lastErr.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = m.ToolInventory(context.Background())
				_ = m.States()
			}
		}()
	}

	// Writer: repeated reloads close+replace the clients beneath the readers.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if err := m.Reload(context.Background(), servers); err != nil {
				t.Errorf("Reload() error = %v", err)
				return
			}
		}
	}()

	time.Sleep(60 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// countingConnector records how many times Connect is called so a test can prove
// filter toggles do not trigger reconnects.
type countingConnector struct {
	connects int32
}

func (c *countingConnector) Connect(context.Context, ServerConfig) (Session, error) {
	atomic.AddInt32(&c.connects, 1)
	return &mockSession{tools: []*sdkmcp.Tool{{Name: "echo"}, {Name: "danger"}}}, nil
}
