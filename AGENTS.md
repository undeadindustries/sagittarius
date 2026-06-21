# Sagittarius AI Agent System Prompt & Persona

## Core Identity & Persona

You are a **Senior Golang AI Engineer at Google** with decades of deep systems and application-level engineering experience.

### Professional Traits & Mindset

- **Systems Architect:** Design for high performance, zero-allocation efficiency, thread safety, and resource economy. View code through startup latency, CPU cycles, and memory.
- **Idiomatic Go Purist:** Clear, readable Go. Avoid clever hacks, reflection, and generic interface wrapping unless necessary. Let the type system work for you.
- **Pragmatic Agentic Thinker:** Code modification, compilation, and validation are a continuous loop. Every line must compile, handle errors, and have table-driven unit tests where logic warrants it.

---

## Project Description & Success Definition

### Project Sagittarius

Sagittarius is a **1:1 Go port** of our frozen, customized `gemini-cli` fork
(`gemini-custom-cli` at `/home/rob/src/gemini-cli`). That folder is **static** —
no further updates. What exists there today is the parity target.

The fork orchestrates requests across:

- Google Gemini (native wire format, API key)
- OpenAI-compatible endpoints (OpenAI, OpenRouter, local vLLM — same adapter)
- OpenAI Responses API (GPT-5 / reasoning)

### Picture of Success

End users cannot tell Sagittarius apart from the fork for in-scope behavior:

- **Command parity:** slash commands, flags, settings reload
- **Streaming & TUI parity:** interactive loop, prompt behavior, stream rendering
- **Performance:** faster startup, lower memory, compile-time safety

**Public product:** This will be a widely used open-source CLI. Write code,
tests, security docs, and commit messages accordingly.

---

## Current Project Status

| Field | Value |
|-------|-------|
| **Overall** | Phase 15 complete — Interaction modes |
| **Active phase** | — (post-parity enhancements) |
| **Go toolchain** | **1.26.4** at `/home/rob/local/go1.26.4`, symlinked system-wide via `/usr/local/bin/go`. apt Go 1.22 removed. |
| **Binary name** | `sagittarius` |
| **Module** | `github.com/undeadindustries/sagittarius` |
| **Config** | Shared `~/.gemini/settings.json` with fork where practical |
| **Plan docs** | `docs/plans/` — **gitignored**, local agent context only |

### Phase Progress

| Phase | Name | Status |
|-------|------|--------|
| 01 | Foundation & public repo | Complete |
| 02 | Config & settings bridge | Complete |
| 03 | Secure credentials | Complete |
| 04 | TUI shell (swappable) | Complete |
| 05 | Gemini provider (API key) | Complete |
| 06 | OpenAI-compat providers | Complete |
| 07 | Agent loop & headless `-p` | Complete |
| 08 | Core tools | Complete |
| 09 | Slash commands | Complete |
| 10 | OpenAI Responses API | Complete |
| 11 | Context management | Complete |
| 12 | MCP, skills, extensions | Complete |
| 13 | Sessions & advanced CLI | Complete |
| 14 | Parity validation | Complete |
| 15 | Interaction modes (post-parity) | Complete |

**MVP milestone (Phases 01–05 + 04):** scaffolding, basic TUI, Gemini API key
auth with secure storage, first streamed Gemini response.

**Provider priority:** Gemini (`gemini-apikey`) first, then OpenAI-compat
(`openai` + custom/OpenRouter). Local vLLM deferred until Spark capacity frees —
same Phase 06 code path, different `baseUrl`.

---

## Architectural Decisions (Log)

Decisions are append-only context for future agents. Do not delete; strike through if reversed.

### AD-001 — Parity target is the fork only (2026-06-20)

Match `/home/rob/src/gemini-cli` as frozen at baseline
`0.42.0-nightly.20260428.g59b2dea0e`. No upstream sync. Out-of-scope surfaces
listed under **Deferred Surface Areas**.

### AD-002 — Gemini requires API key (2026-06-20)

No free-tier / consumer OAuth requirement. Users must supply `GOOGLE_API_KEY` or
store a key via secure credential flow. OAuth and Vertex deferred unless pulled
forward in a phase plan.

### AD-003 — URL-based providers are one adapter (2026-06-20)

OpenAI Chat Completions adapter serves OpenAI, OpenRouter, and local vLLM.
Difference is `baseUrl`, credentials, and settings — not separate architectures.
`wireFormat: openai-chat` triggers openai-chat path; local-only context layers
(Phase 11) key off that wire format (fork `isLocalMode()` semantics).

### AD-004 — TUI behind interface (2026-06-20)

`internal/ui.UI` interface; first implementation Bubble Tea. Agent/core packages
must not import Bubble Tea directly so the TUI library can be swapped.

### AD-005 — Secrets never in settings.json (2026-06-20)

Match fork: env var wins, then OS keychain entry `gemini-cli-provider-<id>`,
encrypted file fallback when keychain unavailable (`GEMINI_FORCE_FILE_STORAGE`).

### AD-006 — Plan files stay out of git (2026-06-20)

Phased plans live in `docs/plans/` (see `.gitignore`). Only `AGENTS.md` and
committed user docs track status for the public repo.

### AD-007 — Post-parity Sagittarius features (2026-06-20)

After Phase 14 parity: plan/ask/debug **interaction modes** with per-mode default
models, plus subagent model overrides with defaults. Settings under
`sagittarius.*` namespace. See `docs/plans/phase-15-interaction-modes.md`.

### AD-008 — Encrypted credentials file path (2026-06-20)

File fallback stores AES-256-GCM encrypted credentials at
`~/.gemini/gemini-credentials.json` (or `$GEMINI_CLI_HOME/.gemini/…`), matching
fork FileKeychain layout and encryption for cross-tool compatibility.

### AD-009 — Gemini SDK via google.golang.org/genai (2026-06-20)

Phase 05 uses `google.golang.org/genai` v1.61.0 with `BackendGeminiAPI` and
`GenerateContentStream` (Go 1.23+ `iter.Seq2`). Provider package exposes
domain types + `ContentGenerator`; TUI mapping to `ui.StreamEvent` deferred to
Phase 07 agent loop.

### AD-010 — Unified OpenAI-chat adapter (2026-06-20)

Phase 06 implements `OpenAIChatGenerator` for all `wireFormat: openai-chat`
endpoints (built-in `openai`, `providers.custom.*`, local vLLM). URL
normalization via `ChatCompletionsURL` / `ExtractServerRoot`; optional Bearer
for local auth; XML tool-call fallback (`ParseXMLToolCalls`); model discovery
via `DiscoverModels`; `IsOpenAIChatMode` hook for Phase 11 context layers.

### AD-011 — Agent loop owns stream mapping (2026-06-20)

Phase 07 `internal/agent` owns the turn loop, `provider.StreamResponse` →
`ui.StreamEvent` mapping, GEMINI.md/AGENTS.md discovery, and headless `-p`.
Tool calls emit `StreamToolStart` only (execution Phase 08). Approval mode stub
is `default` only. Agent packages must not import Bubble Tea.

### AD-012 — Core tools in internal/tools (2026-06-20)

Phase 08 `internal/tools` owns built-in wire tools (`read_file`, `write_file`,
`list_directory`, `run_shell_command`, `grep_search`), workspace path validation,
shell safety subset, registry + scheduler, and approval policy subset
(`default`, `autoEdit`, `yolo`). Runner registers tool declarations on every
generate request and loops up to `MaxToolRounds` (10) after function responses.
Interactive TUI confirms destructive tools via `StreamToolConfirm`; headless
auto-denies confirmations in `default`/`autoEdit` unless `yolo`.

### AD-013 — Slash commands in internal/slash (2026-06-20)

Phase 09 `internal/slash` owns command registry, parser, processor, and built-ins
(`/help`, `/quit`, `/provider`, `/model`, `/auth`, `/memory reload`, `/skills reload`
stub). `agent.App` intercepts `/` input before the runner; slash output uses
`ui.StreamInfo`, quit uses `ui.StreamQuit`. Injectable `slash.Deps` + `Hooks` for
tests. Fork rule: Gemini keys via `/auth`, not `/provider set … key`. Reference:
`docs/reference/commands.md`.

### AD-014 — OpenAI Responses adapter separate from chat (2026-06-20)

Phase 10 implements `OpenAIResponsesGenerator` as a sibling of
`OpenAIChatGenerator` (not merged). `wireFormat: openai-responses` uses
`POST /v1/responses`, SSE `response.*` events, flat tool declarations,
`reasoning.effort`, optional `previous_response_id` chaining, and built-in
`openai-responses` (default model `gpt-5-codex`). Session reasoning override
lives in `provider` session state; `/reasoning` slash commands persist via
`providers.<id>.reasoningEffort`. `IsOpenAIResponsesMode` hook returns true
for responses wire format; `IsOpenAIChatMode` stays false on this path (no
Phase 11 client-side local masking).

### AD-015 — Local-context defenses in internal/contextmgmt, openai-chat only (2026-06-20)

Phase 11 ports the fork's local-context defenses into a new package
`internal/contextmgmt` (named to avoid colliding with stdlib `context`). Every
defense is gated on `provider.IsOpenAIChatMode` (the fork's `isLocalMode`): the
agent builds a `*contextmgmt.Manager` only for `wireFormat: openai-chat`, and a
nil/disabled manager is a pure pass-through. **Gemini-native and
openai-responses paths are never masked or compressed client-side** (consistent
with AD-014).

`Manager.PrepareTurn` runs at the top of every tool round (so it is both the
pre-turn and post-tool hook) and applies, in order: (1) **write-file ejection**
(Layer 3 only — fork TODOs #1–#4 deferred), (2) **tool-output masking** (fork
`toolOutputMaskingService` "Hybrid Backward-Scanned FIFO", with
`localMaskingDefaults` scaled to the resolved context limit and floored by
`minProtectionTokens`/`minPrunableTokens`), and (3) **pre-turn budget** +
**adaptive threshold** + **chat compression** (`chatCompressionService`:
`<state_snapshot>` summarize → verify, split-point on `preserveFraction`,
oversized-tool-response truncation with disk offload). The pre-turn budget layer
only *forces* early compression; the normal threshold check still runs when the
budget does not trigger.

**Active model only:** compression/summarization uses the active provider model
via an injected `Summarizer` adapter (`agent.newProviderSummarizer`) — no
secondary/per-utility model routing (fork open TODO deferred). Loop-detection /
next-speaker model selection: the Go port has no separate loop-detector yet, so
there is no secondary model to redirect; the active-model rule is satisfied by
construction and noted as a follow-up for Phase 12+.

**Token counting:** stdlib-only deterministic heuristic in `tokens.go`
(`charsPerToken = 4`, with a higher divisor for ASCII-heavy text and a JSON
structural surcharge for function-call/response parts) — no tokenizer
dependency. Documented as an approximation; budget math consumes it the same way
the fork consumes its tokenizer counts.

**Adaptive state** is per-session and thread-safe (`AdaptiveTracker` with
`sync.Mutex` + ring buffer), not the fork's package-level mutable state.

**Settings** (per-provider under `providers.<id>.*`, fork-compatible leaf names):
`contextLimit`, `compressionThreshold`, `preserveFraction`,
`toolOutputMaskingEnabled`, `toolOutputMaskingProtectionFraction`,
`toolOutputMaskingPrunableFraction`, `toolOutputMaskingProtectLatestTurn`.
Budget/ejection/adaptive run on built-in defaults (not yet user-exposed —
deferred). Per-provider placement (vs. the fork's single global `local.*` block)
follows AD-003.

### AD-016 — MCP, skills, extensions architecture (2026-06-21)

Phase 12 adds four packages wired through `agent.Runtime` and `agent.Catalog`:

| Package | Role |
|---------|------|
| `internal/mcp` | MCP client + manager; stdio (`CommandTransport`), Streamable HTTP, SSE via official `github.com/modelcontextprotocol/go-sdk` v1.6.1; qualified tool names `mcp_<server>_<tool>` |
| `internal/skills` | `SKILL.md` discovery (`~/.gemini/skills`, `~/.agents/skills`, workspace mirrors); session activate tracking |
| `internal/agents` | Stub registry — discovers `.md` agent definitions from user/project/extension paths; execution deferred |
| `internal/extensions` | Stub loader — `~/.gemini/extensions/*/gemini-extension.json`, settings `extensions` passthrough; merges extension MCP servers + skills |

**Tool catalog:** `agent.Catalog` assembles `tools.NewBuiltinRegistry` + `activate_skill` + MCP tools; `Runner.SetRegistry` on reload. Slash hooks: `/mcp reload`, `/skills reload`, `/agents reload`.

**Credentials:** MCP bearer tokens use `credentials.MCPServerServiceName` → `gemini-cli-mcp-<server>` (env → keychain → encrypted file). Header `${ENV}` expansion in `mcp.ExpandEnvVars`. **OAuth MCP flows deferred** (fork `MCPOAuthProvider` not ported).

**Stubbed vs full:** Extension marketplace, policy/rules/checkers, MCP prompts/resources, OAuth, built-in fork skills, agent execution/subagents, filesystem watcher (manual reload only).

**Dependency:** One added dependency — official MCP Go SDK (documented here; stdlib JSON-RPC deemed higher correctness risk for stdio+HTTP).

### AD-017 — Sandbox stub deferral (2026-06-21)

Phase 13 decision: `sandbox.ts` (fork's Seatbelt/landlock sandbox wrapper for tool execution) is **not ported**. Rationale: the sandbox is platform-specific (macOS Seatbelt, Linux landlock), requires native syscall bindings, and is an execution environment safety feature orthogonal to session persistence. Deferred post-parity. CLI accepts no sandbox-related flags. Document in Phase 14 parity checklist.

### AD-018 — Checkpointing deferred (2026-06-21)

Fork checkpointing (`/restore`) requires a shadow git repository at `~/.gemini/history/<project_hash>` and a checkpoint record format in `~/.gemini/tmp/<hash>/checkpoints/`. The JSONL loader fully supports `$rewindTo` records (written by the recorder when rewinding). However, the shadow-git creation + `/restore` command are **deferred**: they require `os/exec` git subprocess management and a new slash command with significant surface area. Deferred to a follow-up phase. Note `$rewindTo` is read correctly by the session loader now so sessions checkpointed by the fork can be loaded.

### AD-019 — Simplified /resume UI (2026-06-21)

The fork's `/resume` opens an interactive TUI session browser (Bubble Tea list component with search, preview, delete). Phase 13 implements a **text-list** variant instead: `/resume` and `/resume list` print the session list as plain text (same output as `--list-sessions`). The full TUI session browser is deferred to Phase 15 (interaction modes). This is intentionally simpler but fully functional for text-only workflows.

### AD-020 — Git worktrees stub (2026-06-21)

`--worktree` / `-w` flag is accepted and validated against `experimental.worktrees: true` in settings, but git worktree creation (`git worktree add .gemini/worktrees/<name>`) is **not executed**. A clear error message with manual instructions is printed instead. Full implementation requires subprocess management + worktree lifecycle tracking. Deferred to a dedicated worktrees phase post-parity (fork docs: `docs/cli/git-worktrees.md`).

### AD-021 — Parity harness design + fork runnability (2026-06-21)

Phase 14 parity harness lives in `tests/parity/` (package `parity_test`). Mock-server tests run by default via `go test ./...`; live-fork comparison tests are gated by `SAGITTARIUS_PARITY_FORK=1`. Key decisions:

1. **No build tag** — env var gating is simpler and composable with `go test` filters.
2. **Fork invocation:** `npm start -- <args>` from `defaultForkDir = /home/rob/src/gemini-cli`. Noise-stripping regex removes all `> ...`, `Checking build status...`, `npm notice/warn`, and tsx source-rebuild lines.
3. **Mock server:** `net/http/httptest` streaming SSE chunks for OpenAI-chat path. Gemini binary-level mock deferred — SDK uses non-HTTP transport; provider-level tests cover it.
4. **Fork mock comparison:** The fork's `-p hello` against the mock OpenAI server produces only preamble (no AI response) in this environment — the fork's provider code validates credentials or network before reaching the endpoint. Noted as "PARTIAL" in test output; not a failure. Sagittarius correctly returns the mock response.
5. **Performance baseline:** Sagittarius cold-start **7ms**; fork source-run cold-start **~3.6s** (483× speedup). Fork's production bundle broken in this env — source-run is the only option here.
6. **Help parity:** Fork slash commands are not accessible via headless mode; parity is verified via static `forkInScopeCommands` table (extracted from fork TypeScript) + Sagittarius registry comparison.

### AD-022 — Interaction modes vs fork approval modes (2026-06-21)

Phase 15 adds Sagittarius **interaction modes** (`agent`, `plan`, `ask`, `debug`) under
`settings.json` → `sagittarius.*`. They control **model selection** and optional
`systemPromptSuffix` per mode; they do **not** change tool policy.

| Concern | Fork | Sagittarius |
|---------|------|-------------|
| Read-only planning | `--approval-mode plan` (tool policy) | Unchanged — still `ApprovalMode` / tools policy |
| Model routing | Fork auto plan→flash routing in `general.plan.modelRouting` | `/mode` + `sagittarius.modes.*.model` |

**Settings namespace:** `sagittarius.defaultModel`, `sagittarius.defaultMode`,
`sagittarius.modes.{plan,ask,debug,agent}.*`, `sagittarius.subagents.{default,<name>}.*`.
Unknown `sagittarius` sub-keys round-trip via `Extra` maps.

**Default mode when unset:** `agent` (provider default model unless `defaultModel` set).

**Debug mode (Phase 15 MVP):** extra structured logging on model resolution when mode
is `debug`; **does not** port `wireLogger.ts` or disable tool execution (deferred).

**Resolution:** `internal/modes` (`ResolveModel`, `ResolveSubagentModel`); runner
refreshes model before each generate round; `-m` pins model and skips mode routing.
Subagent helper: `agents.ResolveSubagentModel` (execution still stubbed). UX: `/mode`,
TUI **Ctrl+Shift+M** cycles modes. Docs: `docs/interaction-modes.md`.

### AD-023 — Slash autocompletion via ui.Completer seam (2026-06-21)

Inline slash-command autocompletion in the TUI is routed through a new optional
`ui.Completer` interface (`Complete(input string) ui.Completions`) so the Bubble
Tea layer never imports `internal/slash` directly (keeps AD-004 swap boundary).
`agent.App` implements `ui.Completer` by delegating to `slash.Registry.Complete`,
mapping `slash.Suggestion` → `ui.Suggestion`.

`slash.Registry.Complete(input, deps)` is read-only and non-blocking (UI thread,
every keystroke — **no network**): it walks the command tree, returning
command/subcommand candidates, or argument candidates when a leaf command defines
`Command.ArgComplete func(deps, prefix) []string`. Wired arg completers: `/provider
use` (built-in + custom ids) and `/provider remove` (custom only). Model-name
completion is intentionally **not** wired (would require a network `DiscoverModels`
call). `/mode` and `/reasoning` values are already subcommands, so they complete
via the command path.

TUI behaviour (`internal/ui/bubbletea`): suggestions appear under the input when
the line starts with `/`; Up/Down navigate with wrap-around (no auto-highlight —
the user can keep typing); Tab accepts the highlight (or best match); Enter
accepts a highlighted parent (appends a space, reveals subcommands/args) or
submits a terminal command; Esc dismisses. List capped at 8 rows + "… N more".

### AD-024 — Per-model-per-provider settings (design only) (2026-06-21)

Future enhancement, **not implemented** this phase. A provider instance block will
gain an optional `models` map so individual models can override provider-level
defaults (e.g. reasoning effort, temperature):

```json
"providers": {
  "openai": {
    "model": "gpt-4o-mini",
    "models": {
      "gpt-4o-mini": { "temperature": 0.2 },
      "gpt-5-codex":  { "reasoningEffort": "high" }
    }
  }
}
```

Resolution order: `models[activeModel].*` → provider instance defaults → registry
defaults. Mode routing (`sagittarius.modes.*.provider`) layers on top later. The
providers wizard edit sheet would gain a per-model sub-screen. Deferred.

### AD-025 — Providers TUI wizard, `/providers` rename, `/auth` removal (2026-06-21)

The fork's `/provider` command is renamed to `/providers` (plural, no alias) and
the fork's separate `/auth` command is **removed** — API-key entry now lives in
the providers wizard. Both are intentional divergences from the fork, documented
in `docs/PARITY_CHECKLIST.md`.

| Surface | Behaviour |
|---------|-----------|
| `/providers` (no args, TTY) | Opens an interactive Bubble Tea wizard overlay |
| `/providers list\|use\|show\|set\|add\|remove` | Text subcommands retained for scripting (headless never processes slash; these run in the TUI) |
| API keys | Wizard "Set API key" screen, or `/providers set <id> key <api-key>` |

**New package `internal/ui/providersdialog`** (bubbletea-only overlay): a screen
state machine — menu, switch, editPick, edit, editField, setKey, add, addModels,
remove, models. Side effects go through a `providersdialog.Deps` interface
implemented in the agent layer (`agent.App.ProviderDialogDeps`), so the TUI never
imports the agent/slash packages (preserves AD-004 in the other direction).

**Seams:** `slash.Result.OpenDialog DialogKind` (set by the bare `/providers`
handler) → `agent.handleSlash` emits `ui.StreamOpenDialog{Dialog: DialogProviders}`
→ bubbletea opens the overlay via the `providerDialogHost` capability interface.
While the overlay is active the model still routes stream events (e.g. the
terminating `StreamDone`) and window resizes to the host; keys and async
`modelsLoadedMsg` go to the overlay.

**Wire-format field allowlists** (fork `providerRegistry.ts` parity):
`config.ValidSettingKeys(wf)` / `ValidSettingKeysForProvider(id, custom)` —
`gemini` exposes none; `openai-chat` exposes the OpenAI-compat keys plus the
per-provider tool-output masking knobs (AD-015); `openai-responses` swaps
`toolCallParsing` for `reasoningEffort` + `useResponseChaining`.
`provider.ApplyProviderSetting` validates against this allowlist and parses typed
values; `provider.UpdateCustomProviderDefinition` edits definition fields;
`provider.ResolveEndpointForProvider` resolves any provider's endpoint (for model
discovery) without changing the active provider.

**Add flow:** after a custom provider is saved, the wizard immediately calls
`DiscoverModels` and lets the user pick a default model, then auto-switches to the
new provider (matches the user's "immediately connect" requirement).

Tests: `config` ValidSettingKeys per wire format; `provider` ApplyProviderSetting
typed/rejection + UpdateCustomProviderDefinition + ResolveEndpointForProvider;
`providersdialog` table-driven screen transitions with a fake Deps; `bubbletea`
overlay open/close/StreamDone routing. `go test ./...` green, `-race` clean on
touched packages.

---

## Workspace Layout

| Path | Purpose |
|------|---------|
| `/home/rob/src/sagittarius` | This repo (Go port) |
| `/home/rob/src/gemini-cli` | Frozen reference fork (read-only) |
| `docs/plans/` | Local phase plans (gitignored) |
| `AGENTS.md` | **This file** — status, decisions, agent rules |

### Before Implementing Anything

1. Read the relevant **single** phase file in `docs/plans/`.
2. Read the listed Node reference files in the fork.
3. Implement exit criteria + tests for that phase only.
4. Update **this file** phase table and any new AD-* decisions.

---

## Porting Guidelines (Go)

1. **Async → goroutines/channels/`select`**, protect shared state with mutexes.
2. **`context.Context`** on all I/O and long-running loops; clean cancel.
3. **Typed structs** at boundaries; explicit wire-format translation layers.
4. **Errors:** wrap with `%w`, never swallow; fix deprecations and vet findings.
5. **Go version:** verify latest stable at [go.dev/dl](https://go.dev/dl/) each
   session; currently **1.26.4**.
6. **Tooling:** `gofmt`, `go vet`, `go test -race`, `golangci-lint`, `govulncheck`.

### Target Package Layout (Phase 01)

```
cmd/sagittarius/
internal/config/
internal/credentials/
internal/provider/
internal/agent/
internal/tools/
internal/ui/              # ui.UI interface
internal/ui/bubbletea/    # Bubble Tea implementation (only place that imports charm)
internal/ui/demo/         # Phase 04 echo App (replaced in Phase 07)
internal/slash/
internal/modes/             # Phase 15 interaction modes + model routing
internal/mcp/
internal/skills/
internal/agents/
internal/extensions/
internal/session/          # Phase 13 session persistence (JSONL, resume, list)
internal/version/
internal/log/
```

---

Phase 13 complete (2026-06-21): internal/session package (Recorder, LoadSession, ListSessions, DeleteSession, Selector/ResolveSession, ProjectHash/ChatsDir, ConvertToProviderHistory, FormatSessionList); CLI flags --resume/-r, --list-sessions, --delete-session, --output-format text|json|stream-json, --worktree stub (AD-020); session recording wired into agent/runner.go (user/model/tool messages); /resume and /clear slash commands; slash.Hooks extended with ListSessions/ClearHistory; Runner.ClearHistory + InitialHistory; JSONL format fork-compatible. Tests: TestSessionRoundTrip, TestResumeLatest, TestListSessionsEmpty, TestResumeByIndex, TestResumeByUUID, TestResumeInvalidIdentifier, TestProjectHash, TestConvertToProviderHistory, TestDeleteSession, TestFormatSessionList.
Phase 15 complete (2026-06-21): internal/config sagittarius.* typed settings + round-trip; internal/modes (Mode, State, ResolveModel, ResolveSubagentModel); runner mode routing before each generate round; /mode slash + Ctrl+Shift+M TUI cycle; agents.ResolveSubagentModel; docs/interaction-modes.md; AD-022. Tests: TestModeSelectsModel, TestSubagentModelFallback, TestGlobalDefaultWhenUnset, TestModeSwitchMidSession, TestSagittariusSettingsRoundTrip.
Phase 15 Bugbot fixes (2026-06-21): context manager now tracks the runner's live mode-resolved model — NewContextManager takes modelFn func() string (was a captured string) and the summarizer resolves it per call, so chat compression/summarization on the openai-chat path uses the same model as user turns across provider rebuilds AND mid-session /mode switches (was using endpoint.Model / provider default — AD-015 violation); main.go builds the context manager after the runner using runner.Model so startup also matches a non-default mode model; TUI footer refreshes idleStatus from app.Status() on StreamDone so /mode and Ctrl+Shift+M are reflected (was reset to the startup-captured status); ValidateSagittariusSettings no longer rejects a suffix-only mode block (ResolveModel falls back to defaultModel/provider default while the suffix applies); NewRunner no longer treats InitialMode==0 as unset (ModeAgent is the zero value — explicit ModeAgent was silently overridden by sagittarius.defaultMode; callers resolve DefaultFromSettings themselves, as main.go already does). Tests added: TestValidateSuffixOnlyModeAccepted, TestExplicitAgentModeNotOverriddenByDefault. Verified: go test ./... pass, -race clean on touched packages.
Slash autocompletion (2026-06-21, post-Phase 15): inline TUI suggestions as you type a `/` command — Up/Down select (wrap, no auto-highlight), Tab completes, Enter accepts-or-submits, Esc dismisses. New `ui.Completer` seam (bubbletea never imports slash); `agent.App.Complete` → `slash.Registry.Complete`; `Command.ArgComplete` powers value completion for `/providers use` (all ids) and `/providers remove` (custom only). AD-023. Tests: slash completion_test.go (7), bubbletea suggestion nav/accept (6). Model-name arg completion deferred (needs network DiscoverModels). Verified: go test ./... pass, -race clean on slash/agent/bubbletea.

Providers TUI wizard (2026-06-21, post-Phase 15): renamed `/provider` → `/providers` (no alias), removed `/auth` (API keys now in the wizard). New `internal/ui/providersdialog` overlay package (menu/switch/editPick/edit/setKey/add/addModels/remove/models) driven via `providersdialog.Deps` implemented by `agent.App.ProviderDialogDeps`. Seams: `slash.Result.OpenDialog` → `ui.StreamOpenDialog` → bubbletea overlay (`providerDialogHost`). `config.ValidSettingKeys*` wire-format allowlists; `provider.ApplyProviderSetting` (typed+validated), `UpdateCustomProviderDefinition`, `ResolveEndpointForProvider`, `InstanceSettingValues`. Add flow discovers models and prompts for a default, then auto-switches. AD-024 (per-model settings, design only), AD-025. Docs: commands.md, PARITY_CHECKLIST.md updated. Tests: config ValidSettingKeys (4), provider apply/update/resolve (6), providersdialog transitions (6), bubbletea overlay (4). Verified: go test ./... pass, vet clean, -race clean on slash/ui/agent/config.
Next: —
Blockers: fork mock server comparison partial; checkpointing (/restore) deferred (AD-018); sandbox not ported (AD-017); full /resume TUI browser deferred (AD-019); git worktrees stub (AD-020); pre-existing credentials data race ./internal/provider/; TestReasoningApplicableOnResponses flake; debug-mode tool disable + wireLogger port deferred.

Phase 14 complete (2026-06-21): tests/parity/ harness (TestParityHelpOutput, TestParityHeadlessMock, TestParityProviderList, TestParityColdStartPerf); mock OpenAI SSE server (httptest); SAGITTARIUS_PARITY_FORK=1 env gate for live-fork tests; docs/PARITY_CHECKLIST.md committed; AD-021 (parity harness design). Key results: all 4 tests pass, Sagittarius cold-start 7ms vs fork ~3.6s (483x), mock headless response verified end-to-end, all in-scope slash commands present. Known gaps documented in checklist.
Phase 14 Bugbot fixes (2026-06-21): sagittariusBin now builds the binary on demand (go build → temp, cached via sync.Once) instead of t.Skip when bin/sagittarius is absent, so the headless/perf paths always run under `go test ./...` without `make build`; TestParityProviderList now asserts config.LookupBuiltInProvider + BuiltInProviders length instead of only logging the IDs; measureForkColdStart bases its ok return on cmd.Run() success (no bogus perf/speedup log on npm failure or timeout); all fork npm invocations (invokeFork/invokeForkLoose/measureForkColdStart) serialize on forkInvokeMu so t.Parallel tests don't race the shared tsx transpile cache; invokeSagittariusOutput returns exitCode -1 for non-ExitError failures (context deadline, unlaunchable binary) via errors.As so callers gating on code==0 don't misclassify a timed-out run as success. Verified: go test ./tests/parity/ (4 pass, builds bin itself), -race clean, SAGITTARIUS_PARITY_FORK=1 live run (serialized npm, 428x speedup logged).

Next: Phase 15 complete (see handoff above) — post-parity enhancements
Blockers: fork mock server comparison partial (fork exits before reaching mock endpoint — settings format or credential check difference); checkpointing (/restore) deferred (AD-018); sandbox not ported (AD-017); full /resume TUI browser deferred (AD-019); git worktrees stub only (AD-020); pre-existing credentials data race ./internal/provider/; TestReasoningApplicableOnResponses order-dependent flake (internal/slash).

Phase 13 Bugbot fixes (2026-06-21): bare --resume/-r now resumes latest via normalizeResumeArgs (stdlib flag can't express optional-value flags); --resume is a hard error when os.Getwd fails instead of silently starting fresh; ConvertToProviderHistory no longer synthesizes placeholder tool outputs (recorder already persists real responses as the following user turn) — removes duplicate function-response turns on resume; buildProviderParts passes the recorded response map through (coerceResponseMap) instead of double-wrapping under a second "output" key; loadSessionInfo applies $rewindTo trimming so --list-sessions counts/preview match LoadSession; /clear rotates the recorder to a fresh JSONL file (Recorder.Rotate + Runner.RotateSession). Tests added: TestNormalizeResumeArgs, TestConvertToProviderHistoryToolRoundTrip, TestRecorderRotateStartsNewFile, TestListSessionsRespectsRewind.

Phase 12 complete (2026-06-21): internal/mcp (SDK client, manager, DiscoveredTool, credentials bearer), internal/skills (SKILL.md loader/manager), internal/agents (stub registry), internal/extensions (stub loader), agent.Runtime/Catalog, tools.activate_skill, /mcp /skills /agents reload+list, docs/tools/mcp-server.md; tests TestMCPListToolsMock, TestSkillDiscovery, TestActivateSkillTool.
Next: Phase 13 — Sessions & advanced CLI
Blockers: pre-existing data race in credentials.ResetForTesting still surfaces under `go test -race ./internal/provider/`; MCP OAuth, MCP prompts/resources, full /skills enable/disable, extension marketplace deferred.

Phase 11 complete (2026-06-20): internal/contextmgmt package (tokens heuristic, masking_defaults, truncation, tool_output_masking, pre_turn_budget, adaptive_threshold, write_file_ejection, compression, manager) gated by IsOpenAIChatMode; provider.ResolveContextManagement + config masking knobs (toolOutputMasking*); agent.NewContextManager + newProviderSummarizer (active model only); runner pre-turn/post-tool hook via Manager.PrepareTurn; main wiring with per-process sessionID. Tests: write_file_ejection_test (Eject* cases), compression_test (FindCompressSplitPoint + Compress* cases incl. truncation/verification/anchored), tool_output_masking_test, pre_turn_budget_test, adaptive_threshold_test, masking_defaults_test, manager_test (TestManagerMaskingAppliedOnOpenAIChat, TestManagerMaskingNotAppliedWhenDisabled, TestManagerNilIsPassThrough), provider TestResolveContextManagementGating (gemini/openai-responses not masked, openai-chat enabled).
Next: Phase 13 — Sessions & advanced CLI
Blockers: pre-existing data race in credentials.ResetForTesting (hybrid.go:95-98 unguarded globals) surfaces under `go test -race ./internal/provider/` via parallel test cleanups; not Phase 11 code, left untouched per scope. Fix by guarding the sharedFileStore globals with fileStoreMu. Follow-up: dedicated loop-detection/next-speaker port (active-model rule already holds — no secondary model exists yet).

### Built-in fork skills not ported (Phase 12)

Upstream ships one built-in skill in `packages/core/src/skills/builtin/`:

| Skill | Status |
|-------|--------|
| `skill-creator` | **Not ported** — helper scripts (`validate_skill`, `package_skill`, `init_skill`) and full workflow deferred |

User/workspace/extension `SKILL.md` discovery and `activate_skill` **are** ported.

Phase 10 complete (2026-06-20): OpenAIResponsesGenerator + openai_responses_wire (SSE mapper, request translation, chaining trim), built-in openai-responses, ResponsesURL/EndpointConfig reasoning+chaining fields, factory branch, IsOpenAIResponsesMode, session reasoning override, /reasoning slash (show/clear/save/levels), docs/reference/commands.md; tests TestResponsesTextDelta, TestResponsesFunctionCall, TestReasoningEffortInRequest, TestNoLocalMaskingOnResponsesPath, TestFactorySelectsOpenAIResponses, TestReasoningNotApplicableOnGemini, TestReasoningApplicableOnResponses.
Next: Phase 11 — Context management
Blockers: none

Phase 09 complete (2026-06-20): internal/slash (Command, Registry, Parser, Processor, Deps/Hooks), built-ins /help /quit /provider /model /auth /memory reload /skills reload stub, agent.App slash interception, ui StreamInfo/StreamQuit, provider SetProviderModel/Field/AddCustom/RemoveCustom, docs/reference/commands.md; tests TestHelpListsProviderSubcommands, TestProviderUsePersists, TestQuitExits, TestAuthStoresKey, TestProviderSetRejectedForGemini.

Phase 08 complete (2026-06-20): internal/tools (Tool interface, Registry, read_file/write_file/list_directory/run_shell_command/grep_search, path validation, shell safety, Scheduler, policy default/autoEdit/yolo), Runner multi-round tool loop with declarations on GenerateRequest, ui StreamToolConfirm/StreamToolResult, bubbletea y/n confirmation; tests TestReadFileTool, TestWriteFileConfirmation, TestShellBlockedWhenDenied, TestToolSchemaOpenAICompat, TestRipgrepIntegration, TestRunnerToolRoundTrip.

Phase 07 complete (2026-06-20): internal/agent Runner (idle→streaming→awaiting tools→done), ui.App adapter, DiscoverSystemInstruction (GEMINI.md/AGENTS.md + global), MapStreamResponse, headless -p/-m/-d flags, interactive TUI wired to provider stream; tests TestRunnerSingleTurnMock, TestHeadlessPromptFlag, TestCancelMidStream, TestGEMINIMDInjection.

Phase 06 complete (2026-06-20): OpenAIChatGenerator (SSE streaming, XML tool-call fallback, Mistral message patches), EndpointConfig + factory wireFormat branch, DiscoverModels, IsOpenAIChatMode hook, SetActiveProvider/SaveActiveProvider; httptest tests (TestOpenAIChatStream, TestXmlToolCallFallback, TestCustomProviderLoad, TestOpenRouterAsCustom, TestModelDiscoveryEmptyOnFailure, TestFactorySelectsOpenAI).

Phase 05 complete (2026-06-20): internal/provider ContentGenerator + GeminiGenerator (google.golang.org/genai v1.61.0), factory for gemini-apikey, message/tool mapping, user-facing errors, injectable streamer tests (TestGeminiStreamTextDelta, TestGeminiInvalidKey, TestFactorySelectsGeminiAPIKey, TestToolCallRoundTrip).

Phase 04 complete (2026-06-20): internal/ui UI interface + Bubble Tea backend, echo demo App, interactive TTY entry in main, --screen-reader stub, stream events, TestUIRunCancelClean + TestStreamEventRender.

Phase 03 complete (2026-06-20): internal/credentials resolves provider API keys (env → keychain → encrypted file fallback), fork-compatible service naming, 30s read-through cache, Set/Delete APIs, SECURITY.md threat model.

Phase 02 complete (2026-06-20): internal/config loads ~/.gemini/settings.json with typed providers subset, unknown-key passthrough, secret stripping, legacy local.* migration stub, reload notifier stub, built-in gemini-apikey/openai registry.

Phase 01 complete (2026-06-20): Go module, package skeleton, Makefile, CI, public docs, version embedding, TestMainVersion.

---

## Deferred Work

### Fork open TODOs (port later — do not block early phases)

From fork `AGENT.md` → OPEN TODOS:

- Per-utility provider/model routing (compressor, summarizer, …)
- writeFileEjection TODOs #1–#4
- Native Anthropic Messages adapter (`anthropic-messages`)
- AWS Bedrock adapter (`aws-bedrock`)

### Deferred Surface Areas (discuss before porting)

**TODO:** Hold a design session for each before implementation:

- [ ] `packages/vscode-ide-companion`
- [ ] `packages/a2a-server`
- [ ] `packages/sdk`
- [ ] `evals/` and `perf-tests/`
- [ ] Extension marketplace / npm publishing
- [ ] `tools/gemini-cli-bot`

### Slash commands deferred (incremental post-Phase 09)

Track against fork `docs/reference/commands.md`. Implemented subset documented in
`docs/reference/commands.md`.

- [ ] `/about`, `/bug`, `/chat` (alias for `/resume`), `/compress`, `/copy`
- [x] `/resume` (text list, Phase 13) — TUI session browser deferred to Phase 15 (AD-019)
- [x] `/clear` (Phase 13)
- [ ] `/commands`, `/directory`, `/extensions`
- [ ] Full `/skills` enable/disable/link/consent (list + reload implemented Phase 12)
- [ ] `/mcp auth`, `/mcp enable`/`disable` (list + reload implemented Phase 12)
- [ ] `/agents enable`/`disable`/`config` (list + reload implemented Phase 12)
- [x] `/providers` wizard + API-key entry (post-Phase 15, AD-025) — replaces `/provider` and `/auth`
- [ ] `/auth signin`/`signout` OAuth dialogs (OAuth still deferred, AD-002)
- [ ] ACP headless command registry

Implemented in Phase 12: `/mcp` (list, reload), `/skills` (list, reload), `/agents` (list, reload), `activate_skill` tool.

Implemented in Phase 13: `/resume` (text list — TUI browser deferred AD-019), `/clear`.

Implemented in Phase 10: `/reasoning` (show, clear, save, session levels).

### Auth paths deferred

- [ ] Gemini OAuth / Code Assist login
- [ ] Vertex AI (`gemini-vertex`)

---

## Agent Behavioral Directives

- **Research first:** read fork files before writing Go.
- **One phase per session** when possible; update this file before handoff.
- **Compile and test continuously:** `go fmt`, `go test ./...`, `go vet`.
- **Public repo hygiene:** no secrets, SECURITY.md matters, breaking changes documented.
- **Slash commands:** must appear in `/help` with descriptions (fork rule); no `hidden: true` on user commands.

---

## Subagent Handoff Template

When finishing a phase, append to **Current Project Status**:

```
Phase NN complete (YYYY-MM-DD): <one-line summary>
Next: Phase N+1 — <name>
Blockers: <none or list>
```

Update the phase table row to **Complete** or **In progress**.
