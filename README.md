# Sagittarius

Sagittarius is a **Go port** of a customized [gemini-cli](https://github.com/google-gemini/gemini-cli) fork, built for performance, compile-time safety, and parity with the Node reference implementation.

It is an open-source terminal agent CLI that orchestrates requests across:

- **Google Gemini** (native wire format, API key)
- **OpenAI-compatible endpoints** (OpenAI, OpenRouter, local vLLM)
- **OpenAI Responses API** (GPT-5 / reasoning models)

## Status

Early development. Phase 01 provides the repository skeleton, build tooling, and CI. Interactive TUI, providers, and agent loop land in later phases.

## Requirements

- Go **1.26.4** or later ([go.dev/dl](https://go.dev/dl/))

## Build

```bash
make build
./bin/sagittarius --version
```

Or without Make:

```bash
go build -o bin/sagittarius ./cmd/sagittarius
```

## Development

```bash
make test    # unit tests
make vet     # go vet
make lint    # golangci-lint
make race    # race detector
```

Copy `.env.example` to `.env` for local environment variables (never commit `.env`).

## Configuration

Sagittarius reads shared settings from `~/.gemini/settings.json` where practical (Phase 02+). API keys belong in environment variables or OS keychain — not in settings files.

## Relation to gemini-cli

Sagittarius targets behavioral parity with a **frozen fork** used as the reference implementation. It is an independent project, not an official Google product.

## License

Apache License 2.0 — see [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities per [SECURITY.md](SECURITY.md).
