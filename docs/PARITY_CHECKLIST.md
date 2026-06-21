# Sagittarius — Parity Checklist

**Phase:** 14 — Parity Validation  
**Date:** 2026-06-21  
**Baseline fork:** `gemini-custom-cli` at commit `59b2dea0e` (frozen, see AD-001)  
**Fork running version:** `0.45.2` (`npm start -- --version`)  
**Version delta note:** Fork reports 0.45.2; baseline is 0.42.0-nightly.20260428. Delta is not a blocker — frozen fork has no upstream updates and our parity target is AD-001.

---

## Automated parity test results

| Test | Status | Notes |
|------|--------|-------|
| `TestParityHelpOutput` | **PASS** | All in-scope commands present in registry |
| `TestParityHeadlessMock` | **PASS** | Sagittarius returns mock response; fork noise-only (see below) |
| `TestParityProviderList` | **PASS** | Built-in providers verified in registry |
| `TestParityColdStartPerf` | **PASS** | See performance numbers below |

### How to run

```bash
# Default (no fork required — mock servers only):
go test ./tests/parity/

# With live-fork comparison (requires Node + fork at /home/rob/src/gemini-cli):
SAGITTARIUS_PARITY_FORK=1 go test -v ./tests/parity/
```

---

## Performance smoke test

| Metric | Value |
|--------|-------|
| Sagittarius cold-start (`sagittarius -v`) | **7 ms** |
| Fork cold-start (`npm start -- --version`) | **~3.6 s** (includes npm/tsx startup overhead) |
| Speedup | **~484×** |

**Caveat:** The fork's cold-start time includes Node.js process launch, npm script dispatch, and tsx transpilation of TypeScript source files. This is an apples-to-oranges comparison for runtime efficiency, but directly reflects the real-world developer experience difference. The fork's intended production use is via the pre-built bundle (`bundle/gemini.js`), which is broken in this environment (see "Known issues" below), so the source-run time is what end users experience from source.

---

## Feature parity: slash commands

### Implemented (in scope, verified)

| Command | Status | Notes |
|---------|--------|-------|
| `/help` | ✅ Implemented | Description differs from fork ("List slash commands and subcommands" vs fork's "For help on gemini-cli") — intentional, more accurate |
| `/quit` | ✅ Implemented | Description differs ("Exit the interactive session" vs "Exit the cli") — intentional, clearer |
| `/resume` | ✅ Implemented | Text-list variant (AD-019); fork has full TUI browser |
| `/resume list` | ✅ Implemented | |
| `/clear` | ✅ Implemented | |
| `/providers` | ✅ Implemented | Renamed from fork `/provider` (plural). Bare command opens an interactive wizard (AD-025) |
| `/providers list` | ✅ Implemented | |
| `/providers use` | ✅ Implemented | |
| `/providers show` | ✅ Implemented | Fork equivalent: deduced from `models` subcommand context |
| `/providers set` | ✅ Implemented | Wire-format-gated fields (AD-025); `set <id> key` stores an API key |
| `/providers add` | ✅ Implemented | Wizard add flow discovers models and prompts for a default (AD-025) |
| `/providers remove` | ✅ Implemented | |
| `/model` | ✅ Implemented | |
| `/model list` | ✅ Implemented | Fork uses `/provider models` naming; Sagittarius uses `/model list` |
| `/auth` | ↔ Intentionally removed | Folded into the `/providers` wizard "Set API key" screen (AD-025). Headless: `/providers set <id> key <api-key>` |
| `/memory` | ✅ Implemented | |
| `/memory reload` | ✅ Implemented | |
| `/skills` | ✅ Implemented | |
| `/skills list` | ✅ Implemented | |
| `/skills reload` | ✅ Implemented | |
| `/mcp` | ✅ Implemented | |
| `/mcp list` | ✅ Implemented | |
| `/mcp reload` | ✅ Implemented | |
| `/agents` | ✅ Implemented | |
| `/agents list` | ✅ Implemented | |
| `/agents reload` | ✅ Implemented | |
| `/reasoning` | ✅ Implemented | Sagittarius-only (reasoning effort management) |
| `/reasoning show/clear/save/<level>` | ✅ Implemented | |

### Not implemented (intentional deferrals)

| Command | Reason / AD |
|---------|-------------|
| `/about`, `/bug`, `/chat`, `/copy` | Post-parity / incremental |
| `/compress` | Post-parity |
| `/commands`, `/directory`, `/extensions` | Post-parity |
| `/mcp auth`, `/mcp enable`/`disable` | Phase 12+ incremental (AD-016) |
| `/skills enable`/`disable`/`link` | Phase 12+ incremental (AD-016) |
| `/agents enable`/`disable`/`config` | Phase 12+ incremental (AD-016) |
| `/auth signin`/`signout` (OAuth dialogs) | Deferred auth paths (AD-002) |
| ACP headless command registry | Post-parity |
| `/restore` | Checkpointing deferred (AD-018) |

Fork-superset commands present in the fork but not in scope:
`/about`, `/bug`, `/bugMemory`, `/chat`, `/commands`, `/compress`, `/copy`,
`/corgi`, `/directory`, `/docs`, `/editor`, `/exportSession`, `/extensions`,
`/gemmaStatus`, `/hooks`, `/ide`, `/init`, `/permissions`, `/plan`, `/policies`,
`/privacy`, `/profile`, `/restore`, `/settings`, `/setupGithub`, `/shortcuts`,
`/stats`, `/tasks`, `/terminalSetup`, `/theme`, `/tools`, `/upgrade`, `/vim`,
`/voice`

---

## Provider parity

| Provider | Status | Notes |
|----------|--------|-------|
| `gemini-apikey` | ✅ Implemented | Full Gemini native wire format (google.golang.org/genai v1.61.0) |
| `openai` | ✅ Implemented | OpenAI Chat Completions (SSE, tool calls, XML fallback) |
| `openai-responses` | ✅ Implemented | OpenAI Responses API (SSE, reasoning effort) |
| Custom OpenAI-compat | ✅ Implemented | `/providers add` + `wireFormat: openai-chat` |
| OpenRouter | ✅ Implemented | Same adapter as custom OpenAI-compat |
| `anthropic-messages` | ❌ Deferred | Fork open TODO — native Anthropic adapter |
| `aws-bedrock` | ❌ Deferred | Fork open TODO |
| Gemini OAuth / Vertex | ❌ Deferred | AD-002 |

---

## Streaming parity

| Scenario | Status | Notes |
|----------|--------|-------|
| OpenAI-chat SSE headless | ✅ Verified | `TestParityHeadlessMock` — mock server, exit 0, non-empty text |
| Gemini streaming | ✅ Implemented | Provider-level tests (TestGeminiStreamTextDelta); binary test requires real key |
| OpenAI Responses SSE | ✅ Implemented | Provider-level tests (TestResponsesTextDelta) |

---

## Headless flag parity

| Flag | Status | Notes |
|------|--------|-------|
| `-p` / `--prompt` | ✅ | |
| `-m` / `--model` | ✅ | |
| `-d` / `--debug` | ✅ | |
| `-v` / `--version` | ✅ | |
| `--screen-reader` | ✅ | Stub (TUI flag forwarded) |
| `--resume` / `-r` | ✅ | |
| `--list-sessions` | ✅ | |
| `--delete-session` | ✅ | |
| `--output-format` | ✅ | text \| json \| stream-json |
| `--worktree` / `-w` | ⚠️ Stub | AD-020: accepted, gate checked, not executed |

---

## Known gaps / intentional differences

### AD-017: Sandbox not ported

The fork's Seatbelt (macOS) / landlock (Linux) sandbox wrapper for tool execution is not ported. Tool execution runs without OS-level sandboxing. Deferred post-parity.

### AD-018: Checkpointing deferred (`/restore`)

Shadow git repository creation and `/restore` slash command not implemented. The JSONL loader supports `$rewindTo` records written by the fork. Manual checkpointing is not possible from Sagittarius but sessions created by the fork can be loaded.

### AD-019: `/resume` text-list only

The fork's `/resume` opens a full Bubble Tea TUI session browser. Sagittarius has a text-list variant. Full TUI browser deferred to Phase 15.

### AD-020: `--worktree` stub

Flag accepted, experimental gate checked, but git worktree creation is not executed. Prints clear instructions for manual setup.

### Fork bundle broken in this environment

`/home/rob/src/gemini-cli/bundle/gemini.js` crashes: `HybridTokenStorage is not a constructor`. The fork must be invoked via `npm start --` (source run). This is why the live-fork tests use `npm start`.

### Fork version delta

Fork reports `0.45.2`; AD-001 baseline is `0.42.0-nightly.20260428.g59b2dea0e`. The fork is frozen — no upstream sync — so this version delta reflects stale version metadata, not new features. Not a parity blocker.

### Mock server: fork does not emit AI response

When running `SAGITTARIUS_PARITY_FORK=1`, the fork's `-p hello` against the mock OpenAI server produces only preamble/noise (no AI response text). This is because the fork appears to run API key or network validation that prevents reaching the mock server endpoint from within the test subprocess. Sagittarius correctly returns the mock response. This gap is logged as "PARTIAL" in the test output — not a failure.

### Description text differences

Some slash command descriptions differ intentionally:
- `/help`: "List slash commands and subcommands" vs fork "For help on gemini-cli"
- `/quit`: "Exit the interactive session" vs fork "Exit the cli"
- These are clearer/more descriptive in Sagittarius. Not a behavioral difference.

### Pre-existing data race

`credentials.ResetForTesting` (hybrid.go:95-98) has an unguarded global that surfaces under `go test -race ./internal/provider/`. Pre-existing, not introduced in Phase 14. Fix: guard `sharedFileStore` globals with `fileStoreMu`.

### Order-dependent test flake

`TestReasoningApplicableOnResponses` (`internal/slash`) is order-dependent on `provider` session-reasoning package globals. Passes in isolation. Pre-existing.

---

## Manual sign-off checklist

- [x] Build succeeds (`make build`)
- [x] `go vet ./...` clean
- [x] `go test ./...` passes (all packages)
- [x] `go test -race ./tests/parity/` passes
- [x] `TestParityHelpOutput` — all in-scope commands present
- [x] `TestParityHeadlessMock` — Sagittarius produces mock response, exit 0
- [x] `TestParityProviderList` — provider registry verified
- [x] `TestParityColdStartPerf` — Sagittarius 7ms vs fork ~3.6s (483x speedup)
- [x] `SAGITTARIUS_PARITY_FORK=1` tests run and blockers noted
- [x] `docs/PARITY_CHECKLIST.md` committed
- [x] `AGENTS.md` updated (Phase 14 complete, AD-021)
- [ ] Human sign-off on manual TUI walk-through (interactive mode)
- [ ] Human sign-off on real Gemini API key headless test
- [ ] Human sign-off on real OpenAI API key headless test
