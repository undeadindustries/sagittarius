package selfupdate

import (
	"fmt"
	"strings"
)

// AssetName reproduces the GoReleaser template exactly:
// sagittarius_{version}_{os}_{arch}.tar.gz
func AssetName(version, goos, goarch string) string {
	version = strings.TrimPrefix(version, "v")
	return fmt.Sprintf("sagittarius_%s_%s_%s.tar.gz", version, goos, goarch)
}
