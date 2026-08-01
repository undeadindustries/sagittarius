package selfupdate

import "testing"

func TestIsNewer(t *testing.T) {
	for _, tc := range []struct {
		current string
		latest  string
		want    bool
	}{
		{"0.1.0", "0.2.0", true},
		{"v0.1.0", "v0.2.0", true},
		{"0.2.0", "0.1.0", false},
		{"v0.2.0", "v0.2.0", false},
		{"0.1.1", "0.1.2", true},
		{"v0.1.2", "v0.1.1", false},
		{"0.1", "0.1.1", true},
		{"0.1.0", "0.1", false},
		{"dev", "v1.0.0", false},
		{"", "v1.0.0", false},
		{"v1.0.0", "bad", false},
		{"bad", "v1.0.0", false},
	} {
		if got := IsNewer(tc.current, tc.latest); got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v; want %v", tc.current, tc.latest, got, tc.want)
		}
	}
}
