package providersdialog

import "testing"

func TestURLHasPort(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"http://127.0.0.1:8000":  true,
		"http://127.0.0.1":       false,
		"https://api.example.com": false,
		"127.0.0.1:8000":         true,
		"127.0.0.1":              false,
		"localhost:11434":        true,
		"localhost":              false,
		"":                       false,
	}
	for in, want := range cases {
		if got := urlHasPort(in); got != want {
			t.Errorf("urlHasPort(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestComposeAddURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		hostOrURL string
		port      string
		want      string
		wantErr   bool
	}{
		{name: "bare host with embedded port keeps it", hostOrURL: "127.0.0.1:8000", want: "http://127.0.0.1:8000"},
		{name: "bare host with embedded port ignores default", hostOrURL: "127.0.0.1:8000", port: "", want: "http://127.0.0.1:8000"},
		{name: "bare host no port defaults 8000", hostOrURL: "127.0.0.1", want: "http://127.0.0.1:8000"},
		{name: "bare host explicit port", hostOrURL: "127.0.0.1", port: "1234", want: "http://127.0.0.1:1234"},
		{name: "full url with port", hostOrURL: "http://127.0.0.1:8000", want: "http://127.0.0.1:8000"},
		{name: "full url no port left as-is", hostOrURL: "https://api.example.com", want: "https://api.example.com"},
		{name: "full url no port honors explicit port", hostOrURL: "http://127.0.0.1", port: "9000", want: "http://127.0.0.1:9000"},
		{name: "empty errors", hostOrURL: "", wantErr: true},
		{name: "bad scheme errors", hostOrURL: "ftp://host", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := composeAddURL(tc.hostOrURL, tc.port)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("composeAddURL(%q,%q) = %q, want error", tc.hostOrURL, tc.port, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("composeAddURL(%q,%q) unexpected error: %v", tc.hostOrURL, tc.port, err)
			}
			if got != tc.want {
				t.Errorf("composeAddURL(%q,%q) = %q, want %q", tc.hostOrURL, tc.port, got, tc.want)
			}
		})
	}
}
