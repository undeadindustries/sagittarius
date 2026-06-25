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

Sagittarius started as a 1:1 Go port of gemini-cli. Gemini-cli was discontinued and Antigravity is too buggy. Saggitarius has since been updated with many more features: support for Gemini API key, OpenRouter, OpenAI and custom/local AI providers. You can set specific models for different modes (agent, plan, ask). You can set different system prompts (programmer, system admin, personal assistant, creative assistant). Where supported, you can customize temperature and other settings.

### Picture of Success

A bug free, safe alternative to Gemini-cli, Agy, and Opencode to build large projects, admin your system or be your assistant.

This is a public, open-source CLI: write code, tests, security docs, and commit
messages accordingly.

---

## Current State (2026-06-22)

Feature-complete and stable. `go build ./...`, `go vet ./...`, and `go test ./...`
are green; `-race` is clean on actively-touched packages. The codebase is no
longer organized around "phases" — it is a maintained product. Detailed change
history lives in git; record significant **new** architectural decisions briefly
in this file (see [Keeping this file current](#keeping-this-file-current)).

### Project facts


| Field        | Value                                                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------------------------------------- |
| Module       | `github.com/undeadindustries/sagittarius`                                                                                   |
| Binary       | `sagittarius`                                                                                                               |
| Go toolchain | 1.26.4 (pinned in `go.mod` and the `Makefile`)                                                                              |
| Global home  | `~/.sagittarius/` (dedicated; `~/.gemini` is never read or migrated)                                                        |
| Config       | `~/.sagittarius/settings.json`; project overrides in `<repo>/.sagittarius/settings.json` (resolution-only, never persisted) |
| Secrets      | Never in `settings.json`: env var → OS keychain → encrypted file fallback                                                   |


### What works today

- **Providers (one set of adapters):**
  - Gemini native (API key) via `google.golang.org/genai`.
  - OpenAI Chat Completions — the single `openai-chat` adapter also serves
  OpenRouter, custom endpoints, and local vLLM (difference is `baseUrl` +
  credentials, not architecture).
  - OpenAI Responses API (GPT-5 / reasoning), with `reasoning.effort` and
  optional response chaining.
- **Interaction modes:** `agent`, `plan`, `ask`, `debug`. Per-mode model routing;
`plan`/`ask` enforce read-only tool gates in the scheduler (`plan` allows
writes only under `docs/plans/`). `/mode`, Ctrl+Shift+M, or `--mode`. Mode
overrides are **provider-qualified** (`provider + model`): entering a mode
can rebuild the generator for a different provider; leaving reverts to the base.
- **Model-first UX:** Selecting a `{Provider}/{Model}` pair atomically drives the
active provider (internal pointer) as a derived side effect — the user never
picks a provider as "active" explicitly.
  - `/providers` — opens directly at the provider list (built-ins first, customs
  alphabetically). Add custom providers via `a` (name → URL/host → conditional
  port → wire → env → key → id); `x` on a custom row shows a delete confirmation
  that cleans up definition, instance overrides, and stored API key. Edit sheet
  for custom providers shows decomposed `URL / host` + `Port` rows instead of a
  raw `baseUrl` field. URL validation and auto-generated IDs (via
  `provider.ClaimCustomProviderID`) on all add paths.
  - `/model` — global `{Provider}/{Model}` picker (menu + autocomplete); selecting
  any entry calls `SelectCurrentModel` which does switch+set-model atomically.
  - `/models` — per-model settings editor: select `{Provider}/{Model}`, then edit
  temperature, contextLimit, reasoningEffort in a submenu.
  - `/system-prompt` — project-wide system-prompt preset picker; saves to
  `<repo>/.sagittarius/settings.json`.
  - `/modes` / `/mode settings` — mode-override editor: assign a `{Provider}/{Model}`
  override to any mode or clear to default.
  - `Ctrl+/` — cycles globally across all activated models (all providers).
  - `initChecked` pre-selects only the configured default model on uncurated providers.
  - Gemini discovery paginates via `nextPageToken` and filters to `gemini-`* ids only.
  - `PruneModeOverrides` is called on `SetActiveModels`/`RemoveCustomProvider` to
  keep mode overrides consistent with available `(provider, model)` pairs.
- **System prompts:** personalities (`programmer`, `sysadmin`,
`personal-assistant`, `creative-assistant`) × variants (`full`/`lite`),
selected via presets. Per-turn temperature + sampling defaults; context-window
auto-detection. Project default via `/system-prompt` (`sagittarius.systemPrompt`
in project settings); provider override still available in `/providers`.
- **Tools:** `read_file`, `write_file`, `list_directory`, `run_shell_command`,
`grep_search`, `run_project_checks`, plus `activate_skill` and MCP tools.
Approval policy `default`/`autoEdit`/`yolo` (`--approval-mode`, `--yolo`/`-y`).
Workspace path validation; a project-boundary option blocks out-of-root
mutations (file writes + a heuristic shell scan).
- **Code quality / verify:** `run_project_checks` auto-detects the stack
(`go.mod`/`package.json`/`pyproject.toml`/`Cargo.toml`) and runs read-only
lint/format-check/typecheck/build checks, reporting missing tools with install
hints. Check-only runs are allowed in plan/ask; `fix=true` (mutating) is blocked
in plan/ask and gated behind `sagittarius.verify.allowFix`. A `verify-after-edit`
skill template + prompt hardening steer the model to verify after edits; Go
intelligence is available via `gopls mcp`. See `docs/code-quality.md`.
- **Local snapshots:** `write_file` changes are captured for `/diff` and `/undo`
(content snapshots, not shadow git). Session JSONL index under
`~/.sagittarius/tmp/<slug>/snapshots/`; replays across processes when the
session id is reused.
- **Sessions:** JSONL persistence; `--resume`/`-r`, `--list-sessions`,
`--delete-session`, `/resume` (text list), `/clear`.
- **MCP / skills / extensions:** MCP client (stdio + HTTP + SSE) with qualified
tool names; `SKILL.md` discovery + `activate_skill`. Agent/extension registries
are partial stubs (discovery + reload; execution/marketplace deferred).
- **Context management:** local-context defenses (masking, compression, budget)
in `internal/contextmgmt`, gated to the `openai-chat` path only.
- **TUI:** Bubble Tea behind the `internal/ui.UI` interface; semantic themes
(default purple + greyscale/`NO_COLOR`), basic markdown, footer telemetry (per-turn
`↑in ↓out` + optional OpenRouter cost; session totals on detail line), exit summary
per-model/per-mode token breakdown with cost column when OpenRouter cost is known.
De-emphasized `You ›` user blocks with per-turn spacing, colorized `write_file`
diffs (confirm preview + result), a multi-line wrapping input (`textarea`) with
`@path/to/file` mention autocompletion, a loaded-`AGENTS.md` banner line, a
color-cycling working spinner, and per-turn cancel (`Esc`; `Ctrl+C` cancels
then quits). In-app conversation scrolling (`PgUp`/`PgDn` + `Shift+Up`/
`Shift+Down`, with a `followBottom` pin) since alt-screen hides native
scrollback; mouse-wheel scrolling is opt-in (off by default so native text
selection works) via `Alt+M` / `/mouse`. Keyboard shortcuts: `Alt+1..4` set
the mode directly, `Ctrl+Shift+M` cycles it, `Ctrl+/` / `Ctrl+Shift+P` cycle
active models forward/back, `Alt+T` cycles the color theme. An optional
auto-hiding "thinking" box (spinner in its border) shows provider reasoning,
opt-in via `ui.showThinking` (`Ctrl+T` toggles live) or a per-provider/model
`showThinking` setting; and a dim separator rule above the input. Tool
confirmations offer Allow once / Allow for this session / No.
Overlay dialogs: providers wizard, global model picker (`modelpickdialog`), per-model
settings editor (`modelsdialog`), mode-override editor (`modesdialog`), project
system-prompt picker (`systempromptdialog`), MCP server wizard (`mcpdialog`),
tool inventory (`toolsdialog`).
- **MCP & tools management:** `/mcp` is a menu-first wizard (add/edit/enable/
disable/remove/reload); settings-owned servers are editable, extension-owned
servers are read-only. Bearer tokens route to the credentials layer, never
`settings.json`. `/tools` lists built-in tools (read-only, labeled) and MCP
tools grouped by server; toggling an MCP tool persists the server's
`includeTools`/`excludeTools` filter. Config writes go through
`config.SetMCPServer`/`RemoveMCPServer`/`SetMCPServerToolFilter`; inventory
comes from `mcp.Manager.ToolInventory` + `tools.Registry.ListEntries`.
- **Headless:** `-p`/`--prompt`, `--output-format text|json|stream-json`
(stream-json emits `text`/`tool_start`/`tool_result`/`info`/`error` lines), and
`--slash` for a single slash command without a TTY.

### Slash commands

`/help`, `/quit`, `/providers` (wizard; also holds API-key entry — no `/auth`),
`/model` (global picker + autocomplete), `/models` (per-model settings editor),
`/system-prompt` (project personality preset), `/modes` (mode-override editor; alias `/mode settings`), `/mode`, `/reasoning`,
`/compress` (summarize the live context to save tokens; openai-chat only),
`/copy` (copy the last assistant response to the clipboard),
`/stats` (live session usage statistics; `session`/`model`/`tools`),
`/init` (analyze the project and generate a tailored AGENTS.md),
`/theme` (show or switch the TUI color theme; `default`/`greyscale`),
`/memory reload`, `/mcp` (wizard; list/reload), `/tools` (inventory; list/desc),
`/skills` (list/reload), `/agents` (list/reload), `/diff`, `/undo`. Naming rule:
singular sets current one (`/model`, `/mode`), plural manages settings
(`/models`, `/modes`).
User commands must appear in `/help` with a description (no hidden commands).

---

## Architecture

Key seams to preserve:

- **UI is swappable.** Agent/core packages must not import Bubble Tea; only
`internal/ui/bubbletea` imports the charm libraries. Everything crosses the
`internal/ui.UI` interface (and optional `ui.Completer`/`ui.MetricsProvider`).
- **One openai-chat adapter** for all URL-based providers; Gemini-native and
OpenAI-Responses are separate wire paths. Client-side context management is
gated to `openai-chat` only — Gemini and Responses are never masked/compressed
client-side.
- **Memory is `AGENTS.md` only** (no `GEMINI.md`): the global
`~/.sagittarius/AGENTS.md` plus a project walk up to the home boundary.
- `**internal/prompt` is a leaf** (imports only `config` + `tools`); nothing
imports it back. Canonical personality/preset/sampling logic lives in `config`
so `provider` and `prompt` can share it without an import cycle.

### Package layout

```
cmd/sagittarius/          # CLI entry, flags, headless + interactive dispatch
internal/config/          # settings.json (typed + unknown-key passthrough), presets, sampling, paths
internal/credentials/     # API key resolution: env → keychain → encrypted file
internal/provider/        # ContentGenerator + gemini / openai-chat / openai-responses adapters
internal/agent/           # turn loop, App (ui adapter), approval, metrics, runtime/catalog
internal/tools/           # built-in tools, path validation, scheduler, project boundary
internal/contextmgmt/     # local-context defenses (openai-chat only)
internal/modes/           # interaction modes + model routing
internal/prompt/          # personality system prompts (leaf)
internal/slash/           # slash command registry/parser/processor
internal/snapshot/        # local file snapshots (/diff, /undo)
internal/session/         # JSONL session persistence, resume/list
internal/storage/         # global home + project slug registry
internal/mcp/ skills/ agents/ extensions/   # MCP + skills (full); agents/extensions partial
internal/ui/              # ui.UI interface (primitives only)
internal/ui/bubbletea/    # Bubble Tea implementation (only charm importer)
internal/ui/theme/ providersdialog/ modelsdialog/ modelpickdialog/ modesdialog/ systempromptdialog/ mcpdialog/ toolsdialog/  # TUI leaves
internal/version/ internal/log/
tests/parity/             # comparison harness (gated by SAGITTARIUS_PARITY_FORK)
tests/e2e/                # subprocess E2E: live (SAGITTARIUS_E2E_LIVE) + mock (SAGITTARIUS_E2E_MOCK)
```

---

## Engineering conventions

1. **Concurrency:** async → goroutines/channels/`select`; guard shared state with
  mutexes. `context.Context` on all I/O and long-running loops; clean cancel.
2. **Boundaries:** typed structs at wire boundaries; explicit wire-format
  translation layers.
3. **Errors:** wrap with `%w`, never swallow; fix deprecations and vet findings.
4. **Tooling, run continuously:** `gofmt`, `go vet ./...`, `go test ./...`,
  `go test -race` on touched packages; `golangci-lint`; `govulncheck`.
5. **Security/hygiene:** no secrets in the repo or `settings.json`; keep
  `SECURITY.md` accurate; document breaking changes.
6. **Paths in this file:** use repo-relative paths (e.g. `internal/agent/`), never
  absolute machine paths.

### Testing the binary headlessly

```bash
sagittarius --yolo --output-format stream-json -p "create hello.txt with content hi"
SAGITTARIUS_SESSION_ID=run1 sagittarius --slash "/diff"   # pin the session to share snapshots
SAGITTARIUS_SESSION_ID=run1 sagittarius --slash "/undo"
make e2e-mock        # deterministic, key-free
make e2e             # live (needs a provider key; cheap models)
```

See `docs/agent-testing.md`, `docs/interaction-modes.md`,
`docs/system-prompts.md`, `docs/snapshots-and-undo.md`, `docs/home-directory.md`,
and `docs/reference/commands.md` for details.

---

## Known limitations / deferred

- **Auth:** API keys only — no Gemini OAuth / Code Assist, no Vertex AI.
- **Sandbox:** the fork's Seatbelt/landlock tool sandbox is not ported.
- **Checkpointing / `/restore`:** shadow-git conversation rewind is deferred
(session JSONL loader reads `$rewindTo`, but `/restore` is not implemented).
- **Git worktrees:** `--worktree`/`-w` is a validated stub (prints manual
instructions; no worktree creation).
- `**/resume`:** text list only; no interactive TUI session browser.
- **System prompts:** `sysadmin`/`creative-assistant`/`personal-assistant` emit a
distinct role preamble over the shared lite core; full bespoke prompt bodies
are still to be authored. No `/personality` command yet (config/preset-driven).
- **Snapshots:** `write_file` only (not `run_shell_command`); MCP writes are not
boundary-checked.
- **Known flakes:** a pre-existing data race in `internal/provider` credential
globals surfaces under `-race` with ambient stored keys; tests pass in
isolation / without keys.

---

## Recent decisions

- **2026-06-25 — Provider streaming hardening (AD-058):** Plan 02 of the
concurrency-cohesion audit. (A) **No goroutine leak on cancel:** every Gemini
adapter channel send now routes through a shared ctx-aware `sendOrDone(ctx, ch, sr)
bool` (returns false on `ctx.Done()`, producer returns) — previously the bare
`ch <- ...` blocked forever on a stopped consumer until the provider timeout
(this also fixes the AD-056 ReasoningDelta/TextDelta sends, which had inherited
the blocking pattern). The openai-chat and openai-responses SSE callbacks and
the chat non-stream retry loop use the same helper. (B) **Per-instance Responses
chaining id:** `lastResponseID` moved from the process-global `defaultSession`
onto `OpenAIResponsesGenerator` (new `chainMu sync.Mutex` + `setLastResponseID`/
`lastID`); the global `SetLastResponseID`/`LastResponseID`/`ClearLastResponseID`
trio and the `ClearLastResponseID()` calls in `SelectCurrentModel`/
`SaveActiveProvider` were removed (a provider switch builds a fresh generator,
which invalidates the id automatically). This stops cross-session/cross-provider
chaining off a stale `previous_response_id`. The `/reasoning` session override
is still a process global (`defaultSession.reasoningOverride`) with a `// TODO`
linking the plan — the clean per-generator rewire crosses the
slash→app→runner→generator seam and risks losing the override on rebuild, so it
is deferred. (C) **Shared transport:** extracted `internal/provider/openai_http.go`
with `postSSE(ctx, client, url, bearer, body)` (the byte-identical `doRequest`
from both openai adapters) and the shared `sendOrDone`; per-adapter SSE parsing
stays separate. Tests: `gemini_cancel_test.go` (infinite streamer + cancel,
asserts the goroutine exits) and `responses_chaining_test.go` (two generators
don't share `lastResponseID`).

- **2026-06-25 — Runner shared-state synchronization (AD-057):** Hardened the
runner/app turn hot path against data races (plan 01 of the
concurrency-cohesion audit). (1) A new `historyMu sync.RWMutex` guards
`history` + `turnCounter`: `History`/`LastAssistantText`/`ClearHistory`/
`ReplaceHistory`/`ForceCompress` and the turn goroutine's appends
(`RunTurn`, `appendModelMessage`, `appendModelParts`, `appendFunctionResponses`,
`buildGenerateRequest`, `prepareContext`) now lock. Critical sections cover only
slice header reads/writes — `prepareContext` and `ForceCompress` snapshot under
the lock, run their (network-bound) context-manager calls off-lock, then publish
under the lock — so the mutex is never held across I/O. (2) `providerDefaultModel`
and `modelPinned` were folded under the existing `modelMu` (they are part of
model resolution, alongside `model`/`system`/`systemBase`/`memory`); a new
unexported `pinned()` reads the flag under `RLock`, `SetProviderDefaultModel`/
`PinModel` write under `Lock`, and `refreshModelFromMode` reads
`providerDefaultModel` under `RLock` before re-resolving. (3) Settings remain a
swap-only pointer under `settingsMu`; `settingsSnapshot`/`SetSettings` now
document the "immutable once handed to the runner" contract (mutators build a
fresh `*Settings` via `config.Documents` and publish via `SetSettings` — no
in-place edits). (4) `App.status` is guarded by a new `statusMu sync.RWMutex`
(`Status()` reads, `RebuildRunner`/`cycleModel`/`SetInteractionMode` writes).
Regression coverage: `internal/agent/runner_race_test.go` exercises concurrent
history, model-field, and status access under `-race`.

- **2026-06-25 — Gemini native thinking support (AD-056):** Wired Gemini
readable thought text into the existing thinking box. `GenerateRequest` gained
`IncludeThoughts bool`; `BuildGenerateContentConfig` sets
`ThinkingConfig{IncludeThoughts: true}` when the flag is set; `buildGenerateRequest`
in the runner resolves `ResolveShowThinking` and threads it through, so `Ctrl+T`
now activates thinking for Gemini just as it does for OpenRouter. The Gemini
stream loop was reworked to iterate `Content.Parts` directly (instead of
`resp.Text()`, which silently skips thought parts): `p.Thought` parts emit
`StreamResponse{ReasoningDelta}` and non-thought parts emit `StreamResponse{TextDelta}`.
`modelPartsAccumulator.add` skips thought parts so they are never replayed in
history (only the `ThoughtSignature` on adjacent content parts matters for
continuity). The `thoughtSignature` round-trip is unchanged. The `ReasoningDelta`
comment and AGENTS.md AD-053 bullet were corrected to reflect that Gemini can
expose readable thoughts when `IncludeThoughts` is requested.
- **2026-06-25 — Dual-scope settings parity with gemini-cli (AD-055):** Full
 implementation of gemini-cli-style global + project settings duality. (1)
 **Engine:** New `config.Documents` (`LoadDocuments`, `mergeSettings`, `Save(scope)`,
 `TargetSettings(scope)`, `IsDefined`) loads both `~/.sagittarius/settings.json`
 and `<repo>/.sagittarius/settings.json`, merges them (project wins; `mcpServers`
 and `providers.<id>.models` shallow-merge; scalars replace), and exposes a single
 `Merged` view to the runtime. `buildRunner` was refactored to use `Documents`;
 `MergeProjectSystemPrompt` pointer overlay removed. (2) **UI primitive:**
 New `internal/ui/scopedialog` package provides `ScopeSelector` (Tab-focused radio
 — Global / Project), `ScopeBadge`, and `ScopeHint` for reuse across overlays.
 (3) **Scoped saves:** `/modes`, `/model` active-set, `/mcp` add/edit, and
 `/system-prompt` dialogs all gained an "Apply to" scope row defaulting to Project;
 `/providers` footer notes that keys and definitions are always global. Dialog deps
 interfaces (`modesdialog.Deps`, `mcpdialog.Deps`, `modelpickdialog.Deps`) gained
 a `config.SettingScope` save parameter. Agent-side implementations use
 `docs.TargetSettings(scope)` and `docs.Save(scope)`. (4) **`/settings` browser:**
 New `internal/ui/settingsdialog` overlay (curated list grouped by category, scope
 radio, `*` on explicitly-set keys, `Ctrl+L` clear-in-scope, inline bool-toggle /
 enum-cycle / text-edit). Wired to `App.SettingsDialogDeps()` and the new
 `/settings` slash command. (5) **Headless scope flags:** New
 `Hooks.SetModeOverride(scope)` backed by `appHooks`; new `/modes override` and
 `/modes clear` text subcommands accept an optional `global|project` trailing
 argument (default project) for CI/scripting use. Docs updated in
 `docs/home-directory.md` and `docs/reference/commands.md`.

- **2026-06-24 — TUI shortcuts, mouse opt-in, log-corruption fix (AD-054):**
Three follow-ups to the scrollback work (AD-053). (1) **Logging no longer
corrupts the alt-screen:** the codebase had no `slog` handler redirect, so the
default handler wrote to stderr — a late cancel-path error (slog-quoted as
`error="...: context canceled"`) overwrote the bottom row of the alt-screen.
`runInteractive` now calls `configureInteractiveLogging`, which points `slog`
at `~/.sagittarius/logs/sagittarius.log` (fallback `io.Discard`, never stderr);
headless paths keep stderr. (2) **Mouse capture is opt-in:** `tea.WithMouseCellMotion`
was removed from the default program options so the terminal's native click-drag
text selection works again; mouse-wheel scrolling is toggled at runtime with
`Alt+M` or the new `/mouse` command (on/off/toggle/show) via a new
`ui.StreamSetMouse` event + `slash.Result.MouseMode` (mirrors the `StreamSetTheme`
plumbing). Keyboard scrollback (`PgUp`/`PgDn`/`Shift+arrows`) is unchanged.
(3) **New keyboard shortcuts:** `Alt+1..4` select agent/plan/ask/debug directly
(`App.SetModeByName` + shared `switchToMode` helper, refactored from
`CycleInteractionMode`; the UI passes a plain string so bubbletea stays free of
the `modes` package); `Alt+T` cycles the color theme (new `ui.ThemeController` /
`App.CycleTheme`, persisted via `SetUITheme`/`Loader.Save`, applied live through
`m.setTheme`); `Ctrl+Shift+P` cycles active models backward (`App.CycleModelReverse`,
sharing `cycleModel(step)` with `CycleModel` via the `wrapIndex` helper).
`Alt+digit` is used because terminals cannot distinguish `Ctrl+digit`. To
support macOS out-of-the-box without requiring terminal re-configuration, the
special characters produced by Mac Option keys (`¡`, `™`, `£`, `¢`, `†`, `µ`)
are accepted as direct aliases for the `Alt` bindings.
- **2026-06-24 — TUI scrollback, thinking box, input separator (AD-053):** Three
Bubble Tea composer improvements. (1) **In-app scrollback:** alt-screen hides
the terminal's native scrollback, so the program handles `tea.MouseMsg`
(wheel) plus `PgUp`/`PgDn` (half page) and `Shift+Up`/`Shift+Down` (line) —
dedicated keys (mouse capture was initially on by default; see AD-054 which
made it opt-in to restore native text selection)
that don't collide with the input cursor or history Up/Down. A new
`followBottom` flag (default true) gates the auto-`GotoBottom` in
`syncViewportContent`: scrolling up unpins it so new turns don't yank the view
away while reading; reaching the bottom (or submitting a turn) re-pins it.
(2) **Optional thinking box:** reasoning is no longer folded into the answer —
`provider.StreamResponse` gained `ReasoningDelta`, `deltaContent` returns only
`Content` (new `deltaReasoning` extracts `reasoning`/`reasoning_content`), the
Responses adapter maps `reasoning_*_text.delta` to it, and the runner forwards
it as a new `ui.StreamReasoningDelta` event. The new
`internal/ui/bubbletea/thinkingbox.go` renders an auto-hiding rounded box with
the working spinner embedded in its top border and a rolling tail of the
reasoning buffer (cleared on `StreamDone`); it replaces the standalone working
line while shown. Visibility is opt-in: a global `ui.showThinking` setting
(toggled live with `Ctrl+T` via the new `ui.ThinkingController` capability,
persisted through `Settings.SetUIShowThinking`) OR a per-provider/model
`showThinking` setting (config round-trip + `/models` row + providers wizard
bool toggle), resolved by `config.ResolveShowThinking` and surfaced via
`ComposerStatus.ShowThinking`. OpenRouter + OpenAI-Responses populate reasoning via their own wire fields;
Gemini native populates it via `Part.Thought` text when `IncludeThoughts` is
set (the `thoughtSignature` bytes are a separate, opaque continuity handle).
`GenerateRequest.IncludeThoughts` is now threaded from `ResolveShowThinking`
so the Gemini adapter requests thought text whenever the thinking box is on;
other adapters ignore the field. (3)
**Separator:** a dim 1-row rule (`separatorRows`) renders directly above the
input box, accounted for in `bodyHeight` chrome. The agent/UI seam holds —
`internal/slash` and `internal/agent` stay Bubble Tea-free; new capabilities
cross the `internal/ui` interface only.
- **2026-06-24 — Universal tool-call/result integrity repair (AD-052):** Fixed
recurring `invalid_request_message_order` 400s from OpenRouter/Mistral
(`Unexpected tool call id <id> in tool results`). Root cause: a turn cancelled
mid-flight, or a long session, could leave the wire history with an orphaned or
mismatched assistant tool_call / tool result pair. Replaced the Mistral-only
`patchOrphanedToolCallsForMistral` with a provider-agnostic
`repairToolCallIntegrity` (`internal/provider/openai_wire.go`) that runs for
every openai-chat provider in `MessagesToOpenAIMessages`: the block of tool
results following an assistant turn is paired *positionally* with that turn's
tool_calls (the scheduler emits calls and responses in the same order, so this
recovers the original content and forces ids to match even when they drifted),
unanswered calls get a synthesized response, extra results are dropped, and
orphan tool results (no preceding assistant tool_calls) are removed. Also
removed `MistralSafeToolCallID` — OpenRouter handles Mistral's 9-char id
translation, so client-side truncation was unnecessary and only risked
call/response id divergence. Verified against live `mistralai/devstral-2512`:
mismatch, orphan-result, and unanswered-call histories all now return 200.
- **2026-06-24 — TUI input/composer parity with gemini-cli (AD-051):** Reworked
the Bubble Tea input box to match gemini-cli's composer. (1) Single mode prompt:
`syncInputPrompt` now uses the textarea's `SetPromptFunc` so only the first
visual row carries `Agent>`  (or `Plan>` , etc.); wrapped/padding rows are blank
indent, eliminating the duplicate `Agent>` row. (2) The box grows one row per
content line up to `maxInputRows` (raised 6 → 10) with no `+1` buffer row;
`inputBoxHeight`/`inputHeight` and `bodyHeight` chrome were realigned (empty
input is exactly one row). (3) Composer status row: a new
`internal/ui/bubbletea/statusrow.go` renders a dim line between the scrollback
and input — tool-approval hint on the left, `N AGENTS.md files · M skills` on
the right — fed by a new optional `ui.ComposerStatusProvider` (implemented by
`App.ComposerStatus`, backed by `Runner.ApprovalMode` + the skill catalog) and
`Options.LoadedMemoryFiles`. (4) Prompt history: new `inputHistory`
(a port of gemini-cli's `useInputHistory`, with per-level draft/edit caching)
drives boundary-aware Up/Down and Ctrl+P/Ctrl+N — arrows move between lines in
multi-line text, jump to line start/end at the edges, then step through history.
(5) Type-while-busy + message queue: the input stays editable during a turn;
Enter/Tab queue a (non-slash) message that is auto-submitted on `StreamDone`,
and Up on an empty input pops the queue back for editing. The agent/UI seam is
preserved — `internal/slash` and `internal/agent` stay free of Bubble Tea; the
composer-status capability crosses the `internal/ui` interface only.
- **2026-06-24 — PTY shell execution + Background process viewer (AD-050):** 
Brought `run_shell_command` to gemini-cli parity and beyond. (1) Shell execution now runs inside a true Pseudo-Terminal (PTY) using `creack/pty` with a headless VT emulator (`charmbracelet/x/vt`), providing accurate formatting and ANSI-stripping for the model's text result while capturing interactive screen updates correctly. (2) Live output streaming: a new `StreamingTool` interface and `ui.StreamToolOutput` event allow the TUI to render tool output dynamically during long executions. (3) The working status label was fixed (`m.runningTool`) to show "Running run_shell_command" instead of "Thinking…". (4) Background process manager: `internal/bgproc` tracks session-scoped background processes, with `&`-child capture via a subshell `jobs -p` trap, solving the hang issues with foreground servers and explicit background requests. (5) Ctrl+B viewer: A new `DialogBackground` overlay (`internal/ui/bgprocdialog`) lists tracked background processes, their uptime, status, and allows viewing their live log output or killing them via process group (`SIGKILL -pgid`).
- **2026-06-23 — First-class web tools mirroring gemini-cli (AD-042):** Added `google_web_search` and `web_fetch` built-in tools. These tools operate independently of the active chat provider by instantiating a dedicated `GeminiUtilityClient` for non-streaming calls, enabling Gemini's native `GoogleSearch` and `URLContext` grounding even when the user is chatting via OpenRouter. Implemented citation insertion from `GroundingMetadata`. Included a native Go HTTP fallback for `web_fetch` with SSRF protection (blocking localhost/RFC1918), rate limiting (10 req/min per host), and HTML-to-text conversion. `google_web_search` is read-only; `web_fetch` requires confirmation. Configuration lives in `sagittarius.web`.
- **2026-06-23 — Alphabetize menus & fix suggestion scrolling (AD-049):** (1) 
Added a scrolling window (`suggestionWindow`) to the inline TUI suggestions so 
arrowing past item 8 keeps the highlight visible with `↑ / ↓ N more` indicators. 
(2) Alphabetized all user-facing lists: slash command tree (recursively sorted 
at registration, except `/reasoning`), TUI dialogs (modes, MCP servers, system 
prompts), and map-iteration lists (skills, agents, tools). System prompts use a 
new `config.SortedSystemPromptPresets()` to keep full/lite pairs grouped 
alphabetically. Providers dialog and model pickers retain their built-in-first 
grouping but sort alphabetically within groups.
- **2026-06-23 — Bugbot fixes on the new slash commands (AD-048):** Four review
findings on the `/chat`, `/compress`, and `@`-mention work. (1) `/chat save`
now serialises `Runner.History()` via a new `session.WriteHistory` (the inverse
of `ConvertToProviderHistory`) instead of copying the active session JSONL,
which is empty/partial after a `/chat resume` or `/clear` rotation; the old
`copyFile`/`Runner.SessionFilePath` plumbing was removed. (2) `/chat resume`
now replaces the visible scrollback rather than appending to it: a new
`Result.ClearScrollback` → `ui.StreamClearScrollback` event clears `m.blocks`
before the restored turns repaint. (3) `@`-mention completion uses the real
textarea cursor (`inputByteCursor`, reconstructed from `LineInfo`) instead of
`len(value)`, and `acceptSuggestion` preserves the text after the cursor so a
mid-line completion no longer truncates the suffix. (4) The `/compress`
in-memory/JSONL "desync" is **by design, not fixed**: automatic over-budget
compression rewrites `r.history` the same way and never rewrites the recorder —
the session JSONL is the full append-only transcript, while history is the
live compressed working set. Fix (1) already makes `/chat save` capture the
compressed history, which was the only real consistency gap.
- **2026-06-23 — `/theme` live theme switch + persistence (AD-047):** Added a
`/theme` slash command (`show`/`default`/`greyscale`, plus a `set <name>` and
bare-name root handler with alias resolution) that switches the TUI between the
colored default and greyscale themes live **and** persists the choice to
`ui.theme` in settings so it survives restart. Seam: `internal/slash` validates
plain theme-name string constants (`themeDefault`/`themeGreyscale` + a
`parseThemeName` alias resolver) and never imports `internal/ui/theme` (which
pulls in lipgloss/charm) — `theme.Resolve` runs only in the bubbletea layer.
Persistence flows through a new `config.Settings.SetUITheme` (merges into the
`ui.`* object so `hideBanner`/`hideTips`/unknown keys round-trip; clears the key
for `default`/empty) called by the new `Hooks.SetUITheme` (`*appHooks` →
`Loader.Save`). The live switch flows agent→UI via a new `StreamSetTheme` event
(carrying the name in `StreamEvent.Text`, like `/copy`'s `StreamCopyToClipboard`):
the bubbletea model applies it by swapping `m.th = theme.Resolve(name, false)`
and re-deriving cached input/spinner/welcome styling via a new `applyInputTheme`
helper (extracted verbatim from `newModel`, shared by both paths) then
`syncViewportContent`. The explicit in-session choice intentionally bypasses
`NO_COLOR` (a startup-only signal), so re-selecting `default` re-colors even
when launched with `NO_COLOR`.
- **2026-06-23 — `/stats` live session statistics (AD-046):** Added a `/stats`
slash command that shows the same telemetry as the app exit screen, live,
without quitting, with `session` (default), `model`, and `tools` subcommands.
To keep `internal/slash` UI-free, the rendering lives in a new theme-free
(plain-text, no ANSI) `ui.FormatSessionStats` and is surfaced via the
string-returning `Hooks.SessionStatsText` (implemented by `*appHooks`, which
already imports `internal/ui`). The per-(provider,model,mode) grouping was
extracted from the bubbletea exit screen into a shared `ui.AggregateModelUsage`
(keyed by provider+model, never model alone) so the exit screen and `/stats`
share one implementation; the exit-screen output is byte-identical. The pure
leaf helpers `CompactCount`/`FormatCostUSD`/`FormatDuration`/`ToolCallsSummary`
also moved into `internal/ui/metrics.go` (DRY). `/stats tools` reports only
aggregate tool-call counts (calls / failures / success rate) because
Sagittarius does not track per-tool granularity yet.
- **2026-06-23 — `/init` AI-driven AGENTS.md (AD-045):** Added a `/init` slash
command that, matching gemini-cli, is AI-driven rather than a static template:
it creates an empty `AGENTS.md` in the workspace root (no-op when one already
exists) and then submits an analysis prompt instructing the agent to explore
the project with its tools and write a comprehensive `AGENTS.md`. Implemented
via a new `slash.Result.SubmitPrompt` field; `App.handleSlash` runs that prompt
through `Runner.RunTurn` and merges the turn's events into the same stream
(RunTurn emits its own terminal `StreamDone`), so the TUI and headless
consumers need no changes.
- **2026-06-23 — `/copy` to clipboard (AD-044):** Added a `/copy` slash command
that copies the last assistant response to the clipboard. New leaf
`internal/clipboard` wraps `atotto/clipboard` (now a direct dep) with an OSC 52
escape-sequence fallback (`Copy`/`Available`/`OSC52Sequence`/`ErrUnavailable`).
The copy is routed through the UI layer — `slash.Result.Clipboard` →
`ui.StreamCopyToClipboard` — so the agent/slash layers stay terminal-free: the
bubbletea model performs the copy via `tea.Printf` for OSC 52 (never raw
`os.Stdout` while rendering, which would corrupt the display), and headless
`runSlash` writes OSC 52 directly to stdout. New `Runner.LastAssistantText` +
`Hooks.LastAssistantText` (pure `lastAssistantText` helper) expose the last
model turn's text.
- **2026-06-23 — `/compress` manual context compression (AD-043):** Added a
`/compress` slash command that summarizes the live conversation on demand.
New exported symbols: `Manager.CompressionAvailable`/`Manager.ForceCompress`
(`internal/contextmgmt`), `Runner.ContextCompressionAvailable`/
`Runner.ForceCompress` (between-turns contract like `ReplaceHistory`, no
history mutex), and the `Hooks.ForceCompressHistory` hook (implemented by
`appHooks` with `formatCompressionResult`). `ForceCompress` bypasses the
budget/threshold checks (`Force: true`) and replaces `r.history` in place.
Because client-side compression is gated to the openai-chat wire format
(AD-015), the command degrades gracefully with a clear message on
gemini-native and openai-responses providers (nil/disabled manager), never
silently claiming success.
- **2026-06-23 — `/chat` gap-closing vs gemini-cli (AD-042):** Closed the
behavioral gaps between Sagittarius's `/chat` and gemini-cli's. (1) `save`
now guards empty conversations and refuses to clobber an existing checkpoint
unless a `force` token is given (slash has no interactive confirm, so a token
replaces gemini's y/n prompt); a best-effort `checkpoint-<tag>.meta.json`
sidecar records the provider+model. (2) `resume` (alias `load`) restores the
prior conversation into the TUI scrollback via a new `ui.StreamScrollback`
event + `slash.Result.Scrollback []ScrollbackEntry` (role-tagged), and warns —
but does not block — on a provider mismatch read from the sidecar (Sagittarius
history is provider-neutral and thought signatures aren't replayed, so a hard
block like gemini-cli's would be user-hostile). (3) `debug` now writes the
wire request to a timestamped `sagittarius-request-*.json` file: a new optional
`provider.WireRequestDebugger` interface (implemented by the openai-chat and
openai-responses generators) emits the exact serialized body, with the
provider-neutral `GenerateRequest` as the fallback for the Gemini genai path.
(4) `share` defaults to `.json`, rejects extensions other than `.md`/`.json`,
and guards empty history. The interactive session-browser dialog for bare
`/chat` remains deferred (text list for now).
- **2026-06-23 — `@path` file mentions + color-cycling spinner (AD-041):**
New leaf package `internal/atmention/` parses gemini-cli-style `@path/to/file`
references (a hand-written scanner, since Go's RE2 has no lookaround) and
injects the referenced file contents into the model-bound user message inside
`--- Content from referenced files ---` delimiters. `Runner.RunTurn` calls
`atmention.Expand` before appending the user turn, so headless and TUI share
one path; scrollback and session JSONL keep the raw text the user typed, while
only the provider message gains the file blocks (not replayed on resume — a v1
limitation). A mention is recognised only when `@` starts a whitespace/
delimiter-bounded token, so emails like `rob@example.com` are left alone;
resolution failures (missing/binary/directory/out-of-workspace) abort the turn
with a surfaced `StreamError`. Per-file 256 KiB / combined 512 KiB caps bound
injected context. The TUI gains an `@` autocompleter via a new optional
`ui.MentionCompleter` interface (`App.CompleteMention` → cached workspace walk
in `atmention.Index`), reusing the existing suggestion UI. Separately, the
working spinner now cycles colors (`theme.SpinnerGradient` + `applySpinnerColor`
on each `spinner.TickMsg`), matching gemini-cli's `GeminiSpinner` ~4s gradient;
greyscale themes keep a static spinner.
- **2026-06-24 — Background shell + foreground auto-background safety net (AD-044):**
 `run_shell_command` now runs every command through one log-file-backed path
 (`shellTool.run`): stdout+stderr redirect to a temp log file, the process is
 started under `context.Background()` (so a survivor outlives the turn), and a
 reaper goroutine `cmd.Wait()`s to avoid zombies. The tool then selects on three
 outcomes — process exits within the grace window (return output + `exit_code`,
 remove log), ctx canceled (SIGKILL the process group, return ctx err), or grace
 elapses while still running (leave it running, return `{pid, log_file,  background:true}` + captured startup output). The grace window is
 `backgroundStartGrace` (750ms) when `is_background=true`, else
 `autoBackgroundAfter` (default 30s). The 30s foreground threshold is the key
 fix: a server launched WITHOUT `is_background` (e.g. `python3 -m http.server`,
 which a smaller model often forgets to background) used to block the turn
 forever in `cmd.Wait()`; it is now auto-moved to the background so a result
 always returns. Using a log file (not a pipe) means the child keeps writing
 after the tool returns with no SIGPIPE risk and no copy-goroutine leak. The old
 `WaitDelay` band-aid is gone. New `internal/tools/shell_test.go` covers
 foreground capture, non-zero exit, the `&`-no-hang regression, explicit-
 background return-immediately + process-alive + startup capture, immediate-
 failure reporting, cancel-during-grace, foreground auto-background (shortened
 threshold), under-threshold synchronous completion, and an end-to-end
 `python3 -m http.server` TCP-reachability test.
- **2026-06-24 — `--resume` scrollback replay + stale session ID fix (AD-043):**
 `--resume` now replays the loaded conversation into the TUI scrollback so the
 user can see the prior turns immediately (not just have them silently in model
 context). `ui.Options.InitialScrollback []ui.ScrollbackEntry` was added;
 `historyToScrollback` in `cmd/sagittarius/main.go` converts `runner.History()`
 to these entries; the Bubble Tea model seeds blocks from the field in its
 constructor. Separately, `App.SessionMetrics()` now reads the recorder's live
 session ID via `Runner.CurrentSessionID()` so the exit summary and `--resume`
 hint stay accurate after `/clear` or `/chat resume` (which rotate the recorder
 to a new UUID, leaving the original PID-based ID pointing at an empty file).
- **2026-06-23 — Generator cache for O(1) mode switches (AD-040):**
`provider.GeneratorCache` (`internal/provider/cache.go`) caches
`ContentGenerator` instances keyed on all material connection parameters
including the resolved credential. `RebuildRunner` in `internal/agent/app.go`
now calls `generatorCache.GetOrCreate` instead of `provider.NewContentGenerator`
directly. Mode switches that return to a previously-used provider are now
sub-millisecond (no DNS/TLS/`genai.NewClient`); the cache self-invalidates on
any credential or endpoint change because those are part of the key.
- **2026-06-23 — TUI UX overhaul (AD-039):** (1) User scrollback blocks are
de-emphasized (`You ›` prefix, grey `UserBody`) with a blank line between
turns so assistant replies stay the focus. (2) `write_file` shows a colorized
unified diff at confirm time and as the result; the pure diff engine moved to
a leaf `internal/diff` package (snapshot keeps a thin `UnifiedDiff` wrapper) so
`internal/tools` can share it without coupling. (3) Tool confirmations are now
a 3-way decision — `ui.ConfirmDecision` (Once/Session/Deny) replaced the
`chan bool`; the `Scheduler` records per-tool "session" grants to skip later
prompts. (4) The launch banner lists loaded `AGENTS.md` files
(`DiscoverMemoryFiles` + `Runner.LoadedMemoryFiles`). (5) The input is now a
wrapping multi-line `textarea` (Enter submits, Alt/Shift+Enter newline).
(6) The working spinner shows an elapsed timer + cancel hint; `Esc` cancels
the in-flight turn and `Ctrl+C` cancels-then-quits (per-turn cancelable
context in the TUI model). New diff/diff-render/confirm tests cover these.
- **2026-06-22 — TUI working indicator, footer layout, no default stream timeout
(AD-038):** (1) Added an animated Braille-dot spinner (bubbles `spinner.MiniDot`,
matching gemini-cli's `dots`) rendered as a working line above the input
(`internal/ui/bubbletea/working.go`); it only ticks while `busy` and shows
`Thinking…` / `Running {tool}`. The old static `"thinking…"` footer text is gone.
(2) Footer line 1 right side is now `{providerDisplayID} - {model}` (e.g.
`openrouter - qwen/qwen3.7-plus`) plus usage; `StatusBar.Left` is reserved for
transient states (`confirm tool`, `mode`, `model`). `App.providerDisplay` backs
the exit-summary Provider row. (3) `defaultOpenAITimeout` is now `0` (no
client-side stream deadline by default, matching the Gemini path); SIGINT still
cancels, and `providers.<id>.timeout` (seconds) still applies a hard cap when set.
- **2026-06-22 — OpenCode-style verify + diagnostics (AD-037):** Added a thin,
read-only `run_project_checks` built-in tool (`internal/tools/project_checks.go`
  - `internal/tools/checks/` detection) that orchestrates external lint/format/
  typecheck/build CLIs per detected stack (Go, Node/TS, Python, Rust) and reports
  `missing_tools` with install hints — no embedded linters, no native LSP client.
  Check-only is read-only (allowed in plan/ask); `fix=true` is denied in plan/ask
  and gated behind `sagittarius.verify.allowFix` (default off) because formatter
  rewrites are not snapshotted. Prompts (programmer full Validate + lite Verify)
  now teach the discovery-order + install-hint workflow; ships a `verify-after-edit`
  skill template and `docs/code-quality.md`; optional `sagittarius.verify.suggestAfterWrite`
  emits a one-line post-write reminder. Go LSP intelligence is documented via
  `gopls mcp` (reuses the existing MCP client; no new subsystem).
- **2026-06-22 — Gemini thought signatures round-trip (AD-036):** Gemini 3
rejects replayed model `functionCall` parts that drop their
`thoughtSignature` (400 INVALID_ARGUMENT) during multi-step tool loops.
`provider.Part` now carries an optional `ThoughtSignature []byte` and
`StreamResponse` carries optional `ModelParts []Part` (set once on the final
Gemini chunk). `gemini.go` accumulates the full model turn from candidate
parts; `partsToGenai`/`GenaiPartsToParts` round-trip the signature; the runner
stores `ModelParts` verbatim when present (OpenAI-family paths leave it nil and
fall back to text+tool-call reconstruction). The `write_file` ejection rewrite
copies the signature forward. Signatures are not yet persisted to session JSONL
(resume mid-tool-turn remains a follow-up).
- **2026-06-22 — Five Bugbot fixes:** (1) `buildRunner` now resolves `initialMode` and applies any provider override *before* `ResolveEndpointConfig`/`NewContentGenerator`, returning `baseProviderID` to seed `App`; (2) `SetInteractionMode` rewritten deterministically — target = mode's provider OR base, removing the fragile `needProviderRevert` branch; `SelectCurrentModel` sets `baseProviderID` on explicit picks; (3) `ApplySystemPromptPreset` now calls `LookupPreset` and writes canonical `personality` + `promptMode` separately (was storing raw preset id into personality); (4) `fetchGeminiModels` parses the base URL once and uses `q.Set("pageToken", …)` each iteration (was appending, accumulating duplicates from page 3 on); (5) `ResolveContextManagement` takes a `liveModel string` parameter so per-model context-limit lookup uses the mode-resolved live model, not the endpoint's persisted default.
- **2026-06-22 — Provider-reported token usage + OpenRouter cost (AD-035):**
`provider.StreamResponse` now carries an optional `*provider.Usage` (InputTokens,
OutputTokens, CostUSD, CostKnown). All three providers emit it before `Done`:
OpenAI-chat captures the final usage frame (including OpenRouter's `"cost"` field);
Gemini emits `UsageMetadata`; Responses API maps `responsesSseUsage`. The runner
consumes real counts (heuristic fallback when nil) and attributes them keyed by
`(provider, model, mode)`. `sessionMetrics` is rekeyed accordingly and tracks
last-turn and session totals separately. Footer shows `↑in ↓out` (+optional cost)
per turn on line 1, and `Σ session/total` (+optional cost) on line 2. Exit screen
groups by model → mode and shows a Cost column only when any row has `CostKnown=true`.

---

## Keeping this file current

This file is the canonical onboarding doc **and** the agent's injected memory, so
a fresh agent must be able to start from it without asking orientation questions.
When you make a significant change:

- Update the **Current State** section so it reflects reality.
- Record a genuinely new architectural decision as a short dated bullet under a
"Recent decisions" note here (1–3 lines: what changed and why). Don't paste
full diffs or test lists — that's what git is for.
- Keep paths relative and never commit secrets.

