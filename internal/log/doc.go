// Package log provides shared structured logging defaults for Sagittarius libraries.
//
// Library code must use log/slog (via this package or slog directly), not fmt.Println.
package log

import "log/slog"

// Default is the process-wide structured logger used by internal packages.
var Default = slog.Default()
