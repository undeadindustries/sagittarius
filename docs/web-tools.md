# Web Tools

Sagittarius supports first-class web search and web fetch tools, available to every model and provider.

## Available Tools

1. **`google_web_search`**: Searches the web for up-to-date information. It cascades across available backends:
   - **Gemini Google Search Grounding** (preferred when a Gemini API key is configured): Returns cited prose with source links.
   - **Brave Search API** (when a Brave key is configured — see [Brave Search API key](#brave-search-api-key)): Returns structured organic search results.
   - **DuckDuckGo Organic HTML Search** (key-free fallback): Returns organic search results without requiring any API keys.
2. **`web_fetch`**: Fetches content from specified HTTP/HTTPS URLs. It attempts to use Gemini's `URLContext` for optimal extraction and summarization, falling back to a custom, SSRF-protected and rate-limited HTTP fetcher with heuristic HTML-to-text conversion if needed.

## Configuration

Both web tools are enabled by default (`searchEnabled: true`, `fetchEnabled: true`).

You can customize the web tools in your `settings.json`:

```json
{
  "sagittarius": {
    "web": {
      "searchEnabled": true,
      "fetchEnabled": true,
      "directWebFetch": false,
      "utilityModel": "gemini-2.5-flash",
      "maxFetchBytes": 256000
    }
  }
}
```

- **`searchEnabled`**: Controls whether the `google_web_search` tool is registered (default: `true`).
- **`fetchEnabled`**: Controls whether the `web_fetch` tool is registered (default: `true`).
- **`directWebFetch`**: By default, `web_fetch` expects a prompt containing URLs and uses an LLM to summarize the fetched content based on the prompt. Enabling `directWebFetch` changes the tool to accept a single URL parameter and return the raw, converted text without LLM summarization. This is useful for building agents that need to parse raw text themselves.
- **`utilityModel`**: The Gemini model to use for the utility client (default: `gemini-2.5-flash`).
- **`maxFetchBytes`**: The maximum number of bytes to download per fetch request. Defaults to 250 KiB, or 10 MiB in `directWebFetch` mode where the raw text goes to the caller instead of into one turn's context. A zero or negative value is treated as unset.

All of these resolve project-over-global and are re-read when settings are saved, so a `/settings` change takes effect without restarting.

## Brave Search API key

The Brave key is a secret, so it is never stored in `settings.json`. It resolves
the same way a provider API key does:

1. The `BRAVE_API_KEY` environment variable.
2. The OS keychain, or the encrypted file fallback when no keychain is available.

Set it from the TUI under `/settings` → **Secrets** → **Brave Search API key**.
The input is masked, the stored value is never displayed or written to any
settings document, and the row shows only whether a key is present. `Ctrl+L`
removes a stored key.

An environment variable always wins. When `BRAVE_API_KEY` is set, the row says
so, because a key you store there would be shadowed and the search would keep
using the environment value.

Get a key from [Brave Search API](https://brave.com/search/api/). Without one,
search still works — it falls back to DuckDuckGo.

## Security and Confirmation

- **SSRF Protection**: The built-in HTTP fetcher automatically blocks access to localhost, private IP ranges (RFC1918), and loopback addresses to prevent Server-Side Request Forgery.
- **Rate Limiting**: The HTTP fetcher enforces a sliding-window rate limit of 10 requests per minute per host to prevent abuse.
- **Confirmation Policy**: 
  - `google_web_search` is read-only and does not require user confirmation.
  - `web_fetch` is considered an external side-effect and **requires confirmation** in default approval modes, but is automatically allowed in `autoEdit` or `yolo` modes.
- **Interaction Modes**: Both tools are available in read-only interaction modes like `plan` and `ask`.

## Fallback Behavior

- **`google_web_search`**: When a Gemini utility client is configured, searches execute via Gemini GoogleSearch grounding with citations. If no Gemini key is configured, the tool resolves a Brave key from the environment or the secure store; if present, it calls the Brave Search API. If Brave is not configured or errors, it falls back to DuckDuckGo HTML organic search.
- **`web_fetch`**: If a Gemini API key is missing (or if `directWebFetch` is true), the tool falls back to a Go-native HTTP client. This client resolves the URL, enforces SSRF protections, handles retries with exponential backoff for rate limits (HTTP 429) and server errors (HTTP 5xx), and converts the raw HTML response into readable plain text, preserving basic hyperlinks.
