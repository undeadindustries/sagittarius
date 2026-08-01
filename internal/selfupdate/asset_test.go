package selfupdate

import "testing"

func TestAssetName(t *testing.T) {
	for _, tc := range []struct {
		version string
		goos    string
		goarch  string
		want    string
	}{
		{"v0.1.0", "linux", "amd64", "sagittarius_0.1.0_linux_amd64.tar.gz"},
		{"0.2.0", "darwin", "arm64", "sagittarius_0.2.0_darwin_arm64.tar.gz"},
	} {
		if got := AssetName(tc.version, tc.goos, tc.goarch); got != tc.want {
			t.Errorf("AssetName(%q, %q, %q) = %q; want %q", tc.version, tc.goos, tc.goarch, got, tc.want)
		}
	}
}
