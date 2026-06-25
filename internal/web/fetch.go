package web

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// ParsePrompt extracts all http/https URLs from a prompt text, matching
// the heuristic logic from gemini-cli.
func ParsePrompt(text string) (validUrls []string, errs []string) {
	tokens := strings.Fields(text)
	for _, token := range tokens {
		if strings.Contains(token, "://") {
			u, err := url.Parse(token)
			if err != nil {
				errs = append(errs, fmt.Sprintf("Malformed URL detected: %q.", token))
				continue
			}
			if u.Scheme == "http" || u.Scheme == "https" {
				validUrls = append(validUrls, u.String())
			} else {
				errs = append(errs, fmt.Sprintf("Unsupported protocol in URL: %q. Only http and https are supported.", token))
			}
		}
	}
	return
}

// NormalizeURL strips trailing slashes, downcases hostnames, and removes default ports.
func NormalizeURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	parsed.Host = strings.ToLower(parsed.Host)
	if strings.HasSuffix(parsed.Path, "/") && len(parsed.Path) > 1 {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	if parsed.Scheme == "http" && parsed.Port() == "80" {
		parsed.Host = parsed.Hostname()
	} else if parsed.Scheme == "https" && parsed.Port() == "443" {
		parsed.Host = parsed.Hostname()
	}
	return parsed.String()
}

// GitHubBlobToRaw converts github.com/.../blob/... URLs to raw.githubusercontent.com
func GitHubBlobToRaw(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	if parsed.Hostname() == "github.com" {
		parts := strings.Split(parsed.Path, "/")
		// parts: "", "owner", "repo", "blob", "branch", "path..."
		if len(parts) >= 5 && parts[3] == "blob" {
			parsed.Host = "raw.githubusercontent.com"
			newParts := append(parts[:3], parts[4:]...)
			parsed.Path = strings.Join(newParts, "/")
			return parsed.String()
		}
	}
	return u
}

type hostRateLimiter struct {
	mu    sync.Mutex
	hosts map[string][]time.Time
}

var globalRateLimiter = &hostRateLimiter{
	hosts: make(map[string][]time.Time),
}

// Allow limits to 10 requests per minute per host
func (r *hostRateLimiter) Allow(host string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-time.Minute)

	var valid []time.Time
	for _, t := range r.hosts[host] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= 10 {
		return fmt.Errorf("rate limit exceeded for host %s (10 requests per minute)", host)
	}

	valid = append(valid, now)
	r.hosts[host] = valid
	return nil
}

// IsPrivateHost returns true if a hostname resolves to a private IP or loopback.
func IsPrivateHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}

	if hostname == "localhost" {
		return true
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If DNS lookup fails, block to be safe against SSRF
		return true
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}
	return false
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	// Check IANA benchmark testing range (198.18.0.0/15)
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 198 && (ip4[1]&0xFE) == 18 {
			return true
		}
	}
	return false
}

var safeHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			if host == "localhost" {
				return nil, fmt.Errorf("access to private IP blocked: %s", addr)
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			var safeIP net.IP
			for _, ip := range ips {
				if isPrivateIP(ip) {
					return nil, fmt.Errorf("access to private IP blocked: %s resolves to %s", host, ip.String())
				}
				safeIP = ip
			}

			if safeIP == nil {
				return nil, fmt.Errorf("no valid IP for host %s", host)
			}

			var d net.Dialer
			// Connect directly to the resolved safe IP to prevent DNS rebinding
			return d.DialContext(ctx, network, net.JoinHostPort(safeIP.String(), port))
		},
	},
}

// FetchURL fetches a URL using the safe client, enforcing SSRF rules, rate limits,
// and a max bytes cap. It retries 5xx and 429 up to 3 times.
func FetchURL(ctx context.Context, u string, maxBytes int) ([]byte, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	if err := globalRateLimiter.Allow(parsed.Hostname()); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "SagittariusBot/1.0 (+https://github.com/undeadindustries/sagittarius)")

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(1<<attempt) * time.Second):
			}
		}

		resp, err := safeHTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("http %d: %s", resp.StatusCode, resp.Status)
		}

		// Read up to maxBytes + 1
		lr := io.LimitReader(resp.Body, int64(maxBytes)+1)
		data, err := io.ReadAll(lr)
		if err != nil {
			return nil, err
		}
		if len(data) > maxBytes {
			// truncate
			return data[:maxBytes], nil
		}
		return data, nil
	}
	return nil, fmt.Errorf("fetch failed after 3 attempts: %v", lastErr)
}

// HTMLToText is a simple heuristic text extractor that preserves basic links.
func HTMLToText(htmlBytes []byte, baseURL string) string {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		// fallback to raw
		return string(htmlBytes)
	}

	var buf bytes.Buffer
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "script" || n.Data == "style" || n.Data == "head" || n.Data == "noscript" || n.Data == "svg" {
				return
			}
			if n.Data == "a" {
				// extract text and append href
				var href string
				for _, a := range n.Attr {
					if a.Key == "href" {
						href = a.Val
						break
					}
				}

				var innerText bytes.Buffer
				var extractText func(*html.Node)
				extractText = func(cn *html.Node) {
					if cn.Type == html.TextNode {
						innerText.WriteString(strings.TrimSpace(cn.Data))
						innerText.WriteString(" ")
					}
					for c := cn.FirstChild; c != nil; c = c.NextSibling {
						extractText(c)
					}
				}
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					extractText(c)
				}

				text := strings.TrimSpace(innerText.String())
				if text != "" && href != "" {
					buf.WriteString(fmt.Sprintf("%s (%s) ", text, href))
				} else if text != "" {
					buf.WriteString(text + " ")
				} else if href != "" {
					buf.WriteString(href + " ")
				}
				return // Don't recurse normally, we already handled inner text
			}
		}
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				buf.WriteString(text)
				buf.WriteString(" ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	// Clean up extra spaces
	return strings.Join(strings.Fields(buf.String()), " ")
}
