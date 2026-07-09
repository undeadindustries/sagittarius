# How to contribute

Thank you for your interest in Sagittarius. This document covers the contribution
workflow for this Go project.

## Before you begin

1. Read [README.md](README.md) for project scope.
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

Update README.md when behavior, phases, or operational steps change.

## Releasing

To create a new release:

1. Tag the commit using semantic versioning (e.g., `v0.1.0`):
   ```bash
   git tag -a v0.1.0 -m "Release v0.1.0"
   ```
2. Push the tag to GitHub:
   ```bash
   git push origin v0.1.0
   ```
3. The Release GitHub Actions workflow will automatically run, compile the binaries for all target platforms, and publish a draft release on GitHub.
4. Review the drafted release, verify the checksums, and publish it when ready.

## License

By contributing, you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).
