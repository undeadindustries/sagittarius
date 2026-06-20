# How to contribute

Thank you for your interest in Sagittarius. This document covers the contribution
workflow for this Go project.

## Before you begin

1. Read [README.md](README.md) and [AGENTS.md](AGENTS.md) for project scope and phase status.
2. Open an issue or comment on an existing one before large changes.
3. Follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Development setup

1. Install Go **1.26.4+** from [go.dev/dl](https://go.dev/dl/).
2. Clone the repository and enter the project root.
3. Build and verify:

   ```bash
   make build
   ./bin/sagittarius --version
   make test vet lint
   ```

4. Copy `.env.example` to `.env` for local secrets (never commit `.env`).

## Code contribution process

1. Fork the repository and create a feature branch from `main`.
2. Make focused changes with tests where logic warrants them.
3. Run quality checks locally:

   ```bash
   make test vet lint race
   govulncheck ./...
   ```

4. Open a pull request with a clear description and test plan.

## Code style

- Idiomatic Go: explicit error handling, `context.Context` on I/O, minimal dependencies.
- `gofmt` / `goimports` on all Go files.
- Structured logging via `log/slog` in libraries — no `fmt.Println` in library code.
- GoDoc comments on exported symbols.

## Pull request review

All submissions require review via GitHub pull requests. Maintainers may request
changes before merge.

## Documentation

Update README.md and AGENTS.md when behavior, phases, or operational steps change.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
