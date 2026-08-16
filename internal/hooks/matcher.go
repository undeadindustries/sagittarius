package hooks

import (
	"regexp"
	"strings"
)

// Match checks whether a matcher pattern applies to a target string for a given event.
// Empty matcher or "*" matches everything.
// Tool events (BeforeTool, AfterTool) interpret matcher as a regex.
// Lifecycle events interpret matcher as an exact string match.
func Match(event HookEventName, matcher, target string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return true
	}

	if event == EventBeforeTool || event == EventAfterTool {
		re, err := regexp.Compile(matcher)
		if err != nil {
			return false
		}
		return re.MatchString(target)
	}

	// Exact string match for lifecycle events (e.g. source="startup", reason="exit")
	return matcher == target
}
