# Subagents

A subagent is a child agent the model launches to do one bounded job. It gets
its own context window, its own tool loop, and its own session file; the parent
sees nothing but the child's final message.

There are two classes, each with its own switch. Neither is on by default, and
turning on one never turns on the other.

| Class | Tool | Setting | Can write? |
|---|---|---|---|
| Research | `task` | `sagittarius.subagents.research.enabled` | No |
| Coding | `code_task` | `sagittarius.subagents.coding.enabled` | Only inside its declared lease |

Both are live-toggled from `/settings`. The old single `sagittarius.subagents.enabled`
key still works, but only as a fallback for the research switch — it will never
enable coding subagents, because a user who turned on read-only research months
ago did not consent to a child that writes files.

## Research subagents (`task`)

A research subagent runs in ask mode, so the scheduler denies everything outside
the read-only tool set. Use it as a context firewall: a search that would read
forty files pollutes the child's window instead of yours, and you get back a
summary.

The child cannot ask you a question. Its final message is the entire
deliverable, so the prompt has to carry everything it needs.

## Coding subagents (`code_task`)

A coding subagent writes files. Every call must declare `write_paths` — the
paths that child is allowed to modify:

```json
{
  "description": "Add retry to the HTTP client",
  "prompt": "…",
  "write_paths": ["internal/http/**", "internal/http/retry_test.go"]
}
```

Patterns are workspace-relative. `*` matches within one path segment, `**`
matches any depth (including zero), and a trailing `/` means the whole
directory. A write to anything else is denied with `LEASE_DENIED`.

### Why leases

Several coding subagents run at once. Without a declared boundary, two children
editing the same file would each overwrite the other's work, and nothing in the
transcript would say so. The lease turns that race into a refusal you can see:

- **Overlapping leases are rejected before any child starts.** If two `code_task`
  calls in the same turn claim paths that intersect, the whole batch is denied.
  Split the work differently and try again.
- **A write outside the lease is denied**, even when the fix there is obvious.
  The child reports it and the parent decides.
- **The same file is never written by two agents at once.** Writes take a
  per-path lock, so an edit is atomic with respect to siblings.
- **Stale reads are flagged.** If a file you read has been rewritten since, the
  tool result says so and tells you to re-read before editing.

### What a coding subagent cannot do

- **Run mutating shell commands.** Inspection commands (`git status`, `ls`,
  `grep`) run; anything that mutates is denied. A shell command cannot be bound
  by a path lease — `sed -i` anywhere is still a write — so the shell stays
  read-only and the parent runs tests and migrations itself.
- **Launch further subagents.** Depth is capped at one. Neither `task` nor
  `code_task` is registered for a child.
- **Write outside the workspace,** or past the project boundary if that is on.
  The lease narrows the existing gates; it never widens them.
- **Call a write-capable MCP tool.** A leased child cannot bound an MCP write,
  so it is denied. Read-only MCP tools (a search, a calculator) are admitted —
  there is nothing for the lease to bound.

## What a child inherits

A child gets the full context-file stack the parent has: the global and project
`AGENTS.md` walk, both `MEMORY.md` files, the personality system prompt, and its
class charter. It also inherits the parent's standing `/constraints` (a scope
limit you set must bind a leased write), the shared runtime's MCP tools, and
`activate_skill`. It does not get the conversation history (the context
firewall is the point), the parent's scratchpad, or `search_session` — those
are model state and stay behind the firewall.

### Approval

`code_task` asks for confirmation once, showing the lease. Approving the call
approves every write inside it — the child runs unattended, so it cannot stop to
ask. Read the paths before you approve; that prompt is the only place the
boundary is shown before work starts.

Every write a child makes is snapshotted like any other, so `/diff` and `/undo`
cover subagent work the same way they cover your own.

## Model routing

Subagents follow the live `{Provider}/{Model}` pair unless you pin them
elsewhere. Pins are provider-qualified, so a research child can run on a local
model while you work on a cloud one:

```json
{
  "sagittarius": {
    "subagents": {
      "default": { "provider": "openrouter", "model": "qwen/qwen3.8-coder" },
      "coding": { "enabled": true, "provider": "local", "model": "qwen3.8-27b" },
      "research": { "enabled": true }
    }
  }
}
```

Resolution is class pin, then `subagents.default`, then the live pair. This
matters when the mode you are in routes to a model that handles tool calls
poorly — pin the subagents to one that does not. A legacy model-only pin keeps
working and resolves against the live provider.

`/subagents` edits the five routing slots — default, research, coding,
reviewer, utility — with the same `{Provider}/{Model}` picker `/model` uses
(first row clears back to default). `R` resets every slot in the selected scope
(two-step confirm); `Ctrl+L` clears one row. Headless:

- `/subagents show` — list the five slots with their current pins.
- `/subagents set <slot> <Provider/Model> [global|project]` — pin a slot.
- `/subagents clear <slot> [global|project]` — clear one pin.
- `/subagents reset [global|project]` — clear every pin in scope.

The `utility` slot maps to the existing goal evaluator pair
(`evaluatorProvider`/`evaluatorModel`), which already drives the `/goal` judge,
auto-titles, and `/memory compact`. There are no new aux keys: one dialog routes
all off-band model work.

Pins are pruned like mode overrides: deactivate a model or remove a provider
and any pin pointing at it is cleared on the next load. A pin that cannot build
a generator (missing credential, bad endpoint) fails the tool call naming the
slot and `/subagents` — it never silently falls back to the parent's model.

Each child also gets the pinned model's context window, so a small local model
gets masking and compression tuned to its own limits rather than the parent's.

Routing is visible while a child runs: the tool card border carries a dim
`(provider/model)` badge (e.g. `Coding subagent (local/qwen3.8-27b)`) naming
the pair the child will use — pinned or not. The result adds a `via` line only
when the child's pair differs from the parent's (the schema already carries
`provider`/`model`, so a matching `via` would be pure token cost).

## Hand-off schema

A child never reports in prose alone. The harness derives a structured result
from the child's own transcript — files it actually wrote, checks it actually
ran — so the parent can trust the fields without trusting the child's
self-report:

| Field | Source |
|---|---|
| `status` | `completed`, `failed`, or `incomplete` (hit max tool rounds with work done) |
| `summary` | The child's final message |
| `files_changed` | Workspace-relative files the child wrote (from the file registry) |
| `checks` | `{ran, ok}` from the child's last `run_project_checks` response |
| `tool_calls` | How many tool calls the child made |
| `provider`, `model` | The child's resolved pair |
| `attempt`, `max_attempts` | Delegation budget accounting (below) |
| `next_step` | Follow-up instruction for coding results with changed files |
| `review` | Reviewer verdict and findings, when the reviewer pass runs |

`result` and `files_written` remain as aliases. A coding result whose files
changed always carries a `next_step` telling the parent to verify itself: the
child's shell is read-only, so tests are the parent's job whether the child ran
checks or not.

## Delegation budget

Re-delegating the same task counts up. `sagittarius.subagents.maxAttempts`
(default 2, `0` = unlimited) caps how many times one task identity — class plus
description plus lease — may be delegated per session. Past the cap the launch
is refused before any child starts, with a message telling the parent to finish
the task directly. `/clear` and session rotation reset the counts.

## Concurrency

Sibling subagents run together. `sagittarius.subagents.maxConcurrent` (default
8, range 1–16) caps the fan-out; `1` runs children serially, which also reads
as "concurrency disabled". The setting applies live with no rebuild.

## Reviewer pass

`sagittarius.subagents.reviewer.enabled` (default off) adds a read-only review
after a `code_task` child changes files. A reviewer child gets the sibling's
summary, the changed-file list, and per-file diffs from the snapshot index (or
reads the files itself when snapshotting is off), then reports findings plus a
`VERDICT: PASS` / `VERDICT: FAIL` line the harness parses into
`review: {verdict, findings}`. The reviewer never counts toward the attempt
budget, and a reviewer failure is reported rather than fatal. Enable it only if
reviews earn their tokens — a review pass roughly doubles child inference cost.

## When not to use them

A subagent costs a full model round trip and cannot see your conversation. For
multi-step read-only work — grep, then read, then list — `run_script` collapses
the same calls into one turn without spawning anything.
