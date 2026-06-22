package provider

import (
	"fmt"
	"net/url"
	"strings"
)

// ComposeCustomProviderBaseURL builds a canonical base URL from a user-facing
// host-or-full-url and an optional port string. Rules:
//   - If hostOrURL contains "://" it is parsed as a full URL (http/https only).
//     The port field is applied only when the parsed URL has no port.
//   - Otherwise hostOrURL is treated as a bare host/IP: http scheme is assumed
//     and the port field is appended (default "8000" when empty).
func ComposeCustomProviderBaseURL(hostOrURL, port string) (string, error) {
	hostOrURL = strings.TrimSpace(hostOrURL)
	port = strings.TrimSpace(port)
	if hostOrURL == "" {
		return "", fmt.Errorf("host or URL is required")
	}
	if strings.Contains(hostOrURL, "://") {
		u, err := url.Parse(hostOrURL)
		if err != nil {
			return "", fmt.Errorf("invalid URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("URL scheme must be http or https (got %q)", u.Scheme)
		}
		if u.Host == "" {
			return "", fmt.Errorf("URL must include a host")
		}
		if u.Port() == "" && port != "" {
			u.Host = u.Hostname() + ":" + port
		}
		return u.String(), nil
	}
	// Bare host: apply port with default.
	p := port
	if p == "" {
		p = "8000"
	}
	return fmt.Sprintf("http://%s:%s", hostOrURL, p), nil
}

// ParseCustomProviderEndpoint splits a stored base URL into a host-only URL
// (scheme + host + path, no port) and a port string, for edit pre-fill.
func ParseCustomProviderEndpoint(baseURL string) (hostOrURL, port string, err error) {
	u, parseErr := url.Parse(strings.TrimSpace(baseURL))
	if parseErr != nil {
		return baseURL, "", parseErr
	}
	port = u.Port()
	if port != "" {
		u.Host = u.Hostname()
	}
	return u.String(), port, nil
}

// ValidateCustomProviderBaseURL returns an error when baseURL is not a
// parseable http or https URL with a non-empty host.
func ValidateCustomProviderBaseURL(baseURL string) error {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL must include a host")
	}
	return nil
}
