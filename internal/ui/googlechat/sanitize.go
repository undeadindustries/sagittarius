package googlechat

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	// regexBearer matches Authorization Bearer tokens.
	regexBearer = regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9_\-\.]{16,}`)
	// regexAPIKey matches common API key prefixes (Google AIza, OpenAI sk-, etc.).
	regexGoogleKey = regexp.MustCompile(`AIza[0-9A-Za-z-_]{35}`)
	regexOpenAIKey = regexp.MustCompile(`sk-[a-zA-Z0-9_-]{20,}`)
	regexPrivKey   = regexp.MustCompile(`-----BEGIN[ A-Z0-9_-]*PRIVATE KEY-----[\s\S]*?-----END[ A-Z0-9_-]*PRIVATE KEY-----`)
)

// SanitizeToolResult redacts known secret patterns and caps output length to maxRunes.
func SanitizeToolResult(text string, maxRunes int) string {
	if text == "" {
		return ""
	}

	// 1. Redact secrets
	res := text
	res = regexPrivKey.ReplaceAllString(res, "[REDACTED PRIVATE KEY]")
	res = regexBearer.ReplaceAllString(res, "${1}[REDACTED TOKEN]")
	res = regexGoogleKey.ReplaceAllString(res, "[REDACTED API KEY]")
	res = regexOpenAIKey.ReplaceAllString(res, "[REDACTED API KEY]")

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
