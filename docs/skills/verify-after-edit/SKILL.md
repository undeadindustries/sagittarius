---
name: verify-after-edit
description: Keep code IDE-clean by running the project's lint, format check, type check, and tests after edits. Discovers the right tooling for the language, prefers project scripts, and tells the user how to install a missing checker instead of skipping the check.
---

# Verify after edit

Your job is to leave code as clean as it would be in a well-configured IDE:
lint-clean, formatted, type-checked, and building. After you change code, you
verify it the way a careful engineer would before calling the work done.

Prefer the `run_project_checks` tool when it is available: it auto-detects the
stack, runs the read-only checks, and reports any missing tools with an install
hint. Fall back to `run_shell_command` when you need a command the tool does not
cover, or to run a project-specific script.

## When to verify

- After every meaningful code change, before telling the user a task is done.
- A check that passed earlier in the session does NOT cover later edits to the
  same file. Re-verify the final version of every file you changed.
- For pure questions, analysis, or read-only work, do not run checks.

## Discovery order

Find how *this* project wants to be checked, in this order:

1. **Project scripts** — `Makefile` targets (`make lint`, `make test`),
   `package.json` `scripts` (`npm run lint`), `pyproject.toml` `[tool.*]`
   sections, `Cargo.toml`, `.pre-commit-config.yaml`, or CI config. Prefer these
   — they encode the team's intended checks.
2. **Config files** — `.golangci.yml`, `eslint.config.*` / `.eslintrc*`,
   `ruff.toml`, `tsconfig.json`, etc. Their presence tells you which checker the
   project expects.
3. **Language defaults** — when nothing is configured, fall back to the standard
   checker for the language (table below).

## Check bundle

"Clean" usually means more than a linter. When the tools exist, run:

- **Lint** — style and bug patterns (check mode, not auto-fix).
- **Format check** — confirm formatting without rewriting (e.g. `--check`).
- **Type check** — for typed languages (e.g. `tsc --noEmit`, `mypy`).
- **Build / minimal tests** — confirm the change compiles and does not regress.

Run check (non-mutating) variants first. Only run a formatter in write mode when
the user asked you to format, or after you have shown what would change.

## Language defaults

| Marker | Prefer | Language-default CLIs |
|--------|--------|------------------------|
| `go.mod` | `make lint`, `make test` | `gofmt -l .`, `go vet ./...`, `golangci-lint run`, `go build ./...`, `go test ./...` |
| `package.json` | `npm run lint`, `npm test` | `eslint .`, `tsc --noEmit`, `prettier --check .` |
| `pyproject.toml` / `requirements.txt` | project scripts | `ruff check`, `ruff format --check`, `mypy` |
| `Cargo.toml` | `cargo clippy`, `cargo test` | `cargo fmt --check` |

## When a checker is missing

If the project's expected checker is not installed, do not silently skip it. Tell
the user once, with a concrete install command, for example:

> This project has no linter on PATH. If you run `pip install ruff`, I can run
> `ruff check` and `ruff format --check` after edits to keep the code clean.

Then continue with whatever checks you *can* run (build, tests, native
formatters). Do not repeat the same install suggestion every turn.

## Do not

- Do not disable, suppress, or `// nolint` warnings to make a check pass.
- Do not auto-write linter/formatter config files into the project unless asked.
- Do not run mutating formatters (`--write`, `--fix`, `gofmt -w`) without the
  user's intent — those changes are not captured by `/undo`.
