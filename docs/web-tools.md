# Web Tools (gemini-cli parity)

Sagittarius supports first-class web search and web fetch tools, bringing it to feature parity with `gemini-cli`'s "grounded Google Search".

## Available Tools

1. **`google_web_search`**: Leverages Gemini's native Google Search grounding to perform high-quality web searches, automatically returning LLM-formatted results with inline citations and source links.
2. **`web_fetch`**: Fetches content from specified HTTP/HTTPS URLs. It attempts to use Gemini's `URLContext` for optimal extraction and summarization, falling back to a custom, rate-limited HTTP fetcher and heuristic HTML-to-text converter if needed.

## Configuration

These tools are enabled by default if a **Gemini API Key** is configured. Because the highest quality search and fetch capabilities rely on Gemini's native tools, the primary pathways for these tools bypass your active chat provider (e.g., OpenRouter, OpenAI) and make dedicated, non-streaming requests to Gemini directly using a utility client.

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

- **`searchEnabled`**: Controls whether the `google_web_search` tool is registered.
- **`fetchEnabled`**: Controls whether the `web_fetch` tool is registered.
- **`directWebFetch`**: By default, `web_fetch` expects a prompt containing URLs and uses an LLM to summarize the fetched content based on the prompt. Enabling `directWebFetch` changes the tool to accept a single URL parameter and return the raw, converted text without LLM summarization. This is useful for building agents that need to parse raw text themselves.
- **`utilityModel`**: The Gemini model to use for the utility client (default: `gemini-2.5-flash`).
- **`maxFetchBytes`**: The maximum number of bytes to download per fetch request (default: 250 KiB).

## Security and Confirmation

- **SSRF Protection**: The built-in HTTP fetcher automatically blocks access to localhost, private IP ranges (RFC1918), and loopback addresses to prevent Server-Side Request Forgery.
- **Rate Limiting**: The HTTP fetcher enforces a sliding-window rate limit of 10 requests per minute per host to prevent abuse.
- **Confirmation Policy**: 
  - `google_web_search` is read-only and does not require user confirmation.
  - `web_fetch` is considered an external side-effect and **requires confirmation** in default approval modes, but is automatically allowed in `autoEdit` or `yolo` modes.
- **Interaction Modes**: Both tools are available in read-only interaction modes like `plan` and `ask`.

## Fallback Behavior

If `web_fetch` is used but a Gemini API key is missing (or if `directWebFetch` is true), the tool falls back to a Go-native HTTP client. This client resolves the URL, enforces SSRF protections, handles retries with exponential backoff for rate limits (HTTP 429) and server errors (HTTP 5xx), and converts the raw HTML response into readable plain text, preserving basic hyperlinks.
