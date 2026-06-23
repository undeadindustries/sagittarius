# Sagittarius

Sagittarius started as a 1:1 Go port of gemini-cli. Gemini-cli was discontinued and Antigravity is...not ideal. This project has evolved into a bug-free, safe alternative to Gemini-cli, Agy, and Opencode to build large projects, admin your system, or be your assistant.

It is an open-source terminal agent CLI that orchestrates requests across:

- **Google Gemini** (native wire format, API key)
- **OpenAI-compatible endpoints** (OpenAI, OpenRouter, local vLLM, custom/local AI providers)
- **OpenAI Responses API** (GPT-5 / reasoning models)

You can set specific models for different modes (agent, plan, ask), choose different system prompts (programmer, system admin, personal assistant, creative assistant), and customize temperature and other settings.

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

## Configuration & Rules

Sagittarius reads its settings from `~/.sagittarius/settings.json`. API keys belong in environment variables or OS keychain — not in settings files.

### Rules (`AGENTS.md`)

You can define custom rules and instructions that the agent must follow. These are placed in `AGENTS.md` files:

- **Global rules:** Create `~/.sagittarius/AGENTS.md`. The agent will apply these rules across all projects.
- **Project rules:** Create an `AGENTS.md` file in the root of your project. The agent will read this file when run within the project directory.

### Skills (`SKILL.md`)

You can extend the agent's domain knowledge and capabilities by creating skills. Skills are simply Markdown files named `SKILL.md` (or ending in `.md` inside a skill directory). 

**How to create a skill:**
1. Create a new directory for your skill.
2. Inside that directory, create a `SKILL.md` file.
3. Write your instructions, expert context, or playbook for the agent in that Markdown file.

**Where to put skills:**
- **Global skills:** Place them in `~/.sagittarius/skills/` (or `~/.agents/skills/`).
- **Project skills:** Place them in `<your-project>/.sagittarius/skills/` (or `<your-project>/.agents/skills/`).

The agent will automatically discover these skills and can activate them when relevant to your prompt. You can also use the `/skills` command in the CLI to manage them.

A ready-made `verify-after-edit` skill ships in [docs/skills/verify-after-edit/SKILL.md](docs/skills/verify-after-edit/SKILL.md). Copy it into your skills directory to reinforce running lint, format, type-check, and tests after edits.

## Code quality

Sagittarius keeps code IDE-clean by running each project's own lint, format,
type-check, and test tooling — it does not bundle linters. The built-in
`run_project_checks` tool auto-detects the stack and runs its checks, and Go
projects can opt into `gopls` code intelligence over MCP. See
[docs/code-quality.md](docs/code-quality.md).

## License

Apache License 2.0 — see [LICENSE](LICENSE).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). By participating, you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Security

Report vulnerabilities per [SECURITY.md](SECURITY.md).
