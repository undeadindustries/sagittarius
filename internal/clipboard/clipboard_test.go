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

func TestAvailableMatchesAtotto(t *testing.T) {
	t.Parallel()
	if got, want := Available(), !atotto.Unsupported; got != want {
		t.Fatalf("Available() = %v, want %v", got, want)
	}
}
