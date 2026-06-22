package provider

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/config"
)

// CustomIDFromBaseURL derives a short, URL-safe provider id from a base URL
// by lower-casing the host and replacing colons/dots with hyphens.
func CustomIDFromBaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "custom"
	}
	host := strings.ToLower(u.Host)
	host = strings.NewReplacer(":", "-", ".", "-").Replace(host)
	host = strings.Trim(host, "-")
	if host == "" {
		return "custom"
	}
	if len(host) > 32 {
		host = host[:32]
	}
	return host
}

// ClaimCustomProviderID returns a collision-free provider id derived from
// baseURL, appending a numeric suffix when the preferred id is already taken.
func ClaimCustomProviderID(settings *config.Settings, baseURL string) string {
	preferred := CustomIDFromBaseURL(baseURL)
	if settings == nil || settings.Providers == nil || len(settings.Providers.Custom) == 0 {
		return preferred
	}
	if _, taken := settings.Providers.Custom[preferred]; !taken {
		return preferred
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("%s-%d", preferred, i)
		if _, taken := settings.Providers.Custom[id]; !taken {
			return id
		}
	}
}
