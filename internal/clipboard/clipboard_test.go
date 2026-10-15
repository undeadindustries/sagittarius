package clipboard

import (
	"encoding/base64"
	"strings"
	"testing"

	atotto "github.com/atotto/clipboard"
)

func TestOSC52Sequence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "ascii", in: "hello world"},
		{name: "utf8", in: "héllo 世界"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := OSC52Sequence(tt.in)
			want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(tt.in)) + "\a"
			if got != want {
				t.Fatalf("OSC52Sequence(%q) = %q, want %q", tt.in, got, want)
			}
			payload := strings.TrimSuffix(strings.TrimPrefix(got, "\x1b]52;c;"), "\a")
			decoded, err := base64.StdEncoding.DecodeString(payload)
			if err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if string(decoded) != tt.in {
				t.Fatalf("decoded payload = %q, want %q", decoded, tt.in)
			}
		})
	}
}

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestIsRemoteSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"local", map[string]string{"TERM": "xterm-256color"}, false},
		{"ssh_connection", map[string]string{"SSH_CONNECTION": "1.2.3.4 5 6.7.8.9 22"}, true},
		{"ssh_tty", map[string]string{"SSH_TTY": "/dev/pts/3"}, true},
		{"ssh_client", map[string]string{"SSH_CLIENT": "1.2.3.4 5 22"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsRemoteSession(envFrom(tt.env)); got != tt.want {
				t.Fatalf("IsRemoteSession = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOSC52SequenceForEnv(t *testing.T) {
	t.Parallel()
	text := "copy me"
	plain := OSC52Sequence(text)
	b64 := base64.StdEncoding.EncodeToString([]byte(text))
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"plain", map[string]string{"TERM": "xterm-256color"}, plain},
		{
			"tmux emits plain then passthrough",
			map[string]string{"TMUX": "/tmp/tmux-1000/default,1,0", "TERM": "screen-256color"},
			plain + "\x1bPtmux;\x1b\x1b]52;c;" + b64 + "\a\x1b\\",
		},
		{"screen via STY", map[string]string{"STY": "123.pts-0.host"}, "\x1bP\x1b]52;c;" + b64 + "\a\x1b\\"},
		{"screen via TERM", map[string]string{"TERM": "screen"}, "\x1bP\x1b]52;c;" + b64 + "\a\x1b\\"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := OSC52SequenceForEnv(text, envFrom(tt.env)); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOSC52ScreenChunksLongPayload(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("abcdefghij", 20) // 200 bytes -> 268 base64 chars
	got := OSC52SequenceForEnv(text, envFrom(map[string]string{"STY": "x"}))
	body := strings.TrimSuffix(strings.TrimPrefix(got, "\x1bP\x1b]52;c;"), "\a\x1b\\")
	chunks := strings.Split(body, "\x1b\\\x1bP")
	for i, c := range chunks {
		if len(c) > screenChunkSize {
			t.Fatalf("chunk %d len %d exceeds %d", i, len(c), screenChunkSize)
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Join(chunks, ""))
	if err != nil || string(decoded) != text {
		t.Fatalf("reassembled payload mismatch (err=%v)", err)
	}
}

func TestWriteOSC52(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	if err := WriteOSC52(&b, "hi", envFrom(nil)); err != nil {
		t.Fatalf("WriteOSC52: %v", err)
	}
	if b.String() != OSC52Sequence("hi") {
		t.Fatalf("wrote %q", b.String())
	}
}

func TestAvailableMatchesAtotto(t *testing.T) {
	t.Parallel()
	if got, want := Available(), !atotto.Unsupported; got != want {
		t.Fatalf("Available() = %v, want %v", got, want)
	}
}
