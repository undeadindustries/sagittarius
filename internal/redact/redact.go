package redact

import (
	"regexp"
)

var (
	// regexBearer matches Authorization Bearer tokens.
	regexBearer = regexp.MustCompile(`(?i)(bearer\s+)[a-zA-Z0-9_\-\.]{16,}`)
	// regexGoogleKey matches Google AIza API keys.
	regexGoogleKey = regexp.MustCompile(`AIza[0-9A-Za-z-_]{35}`)
	// regexOpenAIKey matches OpenAI sk- API keys.
	regexOpenAIKey = regexp.MustCompile(`sk-[a-zA-Z0-9_-]{20,}`)
	// regexPrivKey matches PEM private keys.
	regexPrivKey = regexp.MustCompile(`-----BEGIN[ A-Z0-9_-]*PRIVATE KEY-----[\s\S]*?-----END[ A-Z0-9_-]*PRIVATE KEY-----`)
	// regexAnthropicKey matches Anthropic sk-ant- keys.
	regexAnthropicKey = regexp.MustCompile(`sk-ant-[a-zA-Z0-9_\-\.]{20,}`)
	// regexGenericSecret matches common secret assignments like api_key=..., secret=...
	regexGenericSecret = regexp.MustCompile(`(?i)(api[_-]?key|secret|password|token)\s*[:=]\s*["']?([a-zA-Z0-9_\-\.]{16,})["']?`)
)

// Secrets replaces known credential, token, and private key patterns with redacted placeholders.
func Secrets(text string) string {
	if text == "" {
		return ""
	}
	res := text
	res = regexPrivKey.ReplaceAllString(res, "[REDACTED PRIVATE KEY]")
	res = regexBearer.ReplaceAllString(res, "${1}[REDACTED TOKEN]")
	res = regexAnthropicKey.ReplaceAllString(res, "[REDACTED API KEY]")
	res = regexGoogleKey.ReplaceAllString(res, "[REDACTED API KEY]")
	res = regexOpenAIKey.ReplaceAllString(res, "[REDACTED API KEY]")
	res = regexGenericSecret.ReplaceAllString(res, "${1}=[REDACTED]")
	return res
}
