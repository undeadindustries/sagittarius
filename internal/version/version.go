// Package version holds build metadata injected at link time via -ldflags.
package version

import "fmt"

var (
	// Version is the release tag or semantic version (default "dev").
	Version = "dev"
	// Commit is the short git commit hash (default "none").
	Commit = "none"
	// BuildDate is the UTC build timestamp (default "unknown").
	BuildDate = "unknown"
)

// String returns a human-readable version line for CLI output.
func String() string {
	if Commit == "none" && BuildDate == "unknown" {
		return Version
	}
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}
