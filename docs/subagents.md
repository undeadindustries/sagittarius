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

### Approval

`code_task` asks for confirmation once, showing the lease. Approving the call
approves every write inside it — the child runs unattended, so it cannot stop to
ask. Read the paths before you approve; that prompt is the only place the
boundary is shown before work starts.

Every write a child makes is snapshotted like any other, so `/diff` and `/undo`
cover subagent work the same way they cover your own.

## Model routing

Subagents use the live model unless you route them elsewhere:

```json
{
  "sagittarius": {
    "subagents": {
      "coding": { "enabled": true, "model": "qwen/qwen3.8-coder" },
      "research": { "enabled": true },
      "default": { "model": "…" }
    }
  }
}
```

Resolution is class model, then `subagents.default.model`, then the live model.
This matters when the mode you are in routes to a model that handles tool calls
poorly — pin the subagents to one that does not.

## When not to use them

A subagent costs a full model round trip and cannot see your conversation. For
multi-step read-only work — grep, then read, then list — `run_script` collapses
the same calls into one turn without spawning anything.
