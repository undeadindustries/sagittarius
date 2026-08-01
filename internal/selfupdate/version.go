package selfupdate

import (
	"strconv"
	"strings"
)

// IsNewer returns true if latest is strictly greater than current.
// Both strings may optionally have a "v" prefix.
// Parses semantic versioning components X.Y.Z.
func IsNewer(current, latest string) bool {
	if current == "dev" || current == "" {
		return false
	}
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")

	cParts := strings.Split(current, ".")
	lParts := strings.Split(latest, ".")

	maxLen := len(cParts)
	if len(lParts) > maxLen {
		maxLen = len(lParts)
	}

	for i := 0; i < maxLen; i++ {
		cPart := 0
		if i < len(cParts) {
			parsed, err := strconv.Atoi(cParts[i])
			if err != nil {
				return false // invalid version, assume not newer
			}
			cPart = parsed
		}
		lPart := 0
		if i < len(lParts) {
			parsed, err := strconv.Atoi(lParts[i])
			if err != nil {
				return false
			}
			lPart = parsed
		}
		if lPart > cPart {
			return true
		}
		if lPart < cPart {
			return false
		}
	}
	return false
}
