package googlechat

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/undeadindustries/sagittarius/internal/redact"
)

// SanitizeToolResult redacts known secret patterns and caps output length to maxRunes.
func SanitizeToolResult(text string, maxRunes int) string {
	if text == "" {
		return ""
	}

	// 1. Redact secrets
	res := redact.Secrets(text)

	// 2. Length cap
	if maxRunes <= 0 {
		maxRunes = 2000
	}

	runeCount := utf8.RuneCountInString(res)
	if runeCount <= maxRunes {
		return res
	}

	runes := []rune(res)
	truncated := string(runes[:maxRunes])
	dropped := runeCount - maxRunes

	return fmt.Sprintf("%s\n\n[... truncated %d characters for Google Chat ...]", strings.TrimRight(truncated, "\r\n"), dropped)
}
