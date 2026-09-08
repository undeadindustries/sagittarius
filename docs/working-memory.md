# Working memory: the scratchpad and session search

Context compression keeps a long conversation inside the model's window by
rewriting old turns into prose. That is the right trade for size, and it is
lossy by design: an exact path, port, id, or decision from two hundred turns ago
survives only as a summary of the topic it appeared in. A model that does not
know this will confidently guess, or ask you to repeat something you already
said.

Two tools close that gap from opposite directions.

- **`update_scratchpad`** keeps a short note *outside* the conversation, so
  compression cannot touch it.
- **`search_session`** reads the full transcript on disk, which still holds
  every word the live history no longer does.

Neither is durable across sessions. That is MemPalace's job (or `save_memory`,
which appends a lasting fact to `AGENTS.md`). These two are for the task
currently in progress.

## The scratchpad

The model calls `update_scratchpad` with the complete new text of its note. The
note is then injected into the system prompt on every subsequent request, which
is why compression, tool-output masking, and truncation cannot reach it — the
system instruction is rebuilt from scratch each turn and never lives in history.

What good use looks like: the plan and which step it is on, the exact paths it
has already changed, a port or container name it discovered, a decision you made
that it must not relitigate.

### Replace, don't append

There is no append mode. The model always reads its current note back in its own
prompt, so it can rewrite the whole thing, and one path is easier to reason
about than two. An empty string clears the note.

### It is the model's note, not yours

The block is framed to the model as *its own* working memory: reference material
it wrote, explicitly not an instruction from you. That framing is load-bearing.
An unlabeled block sitting in the prompt gets copied into the model's visible
replies (see AD-119 and AD-068 in the project's decision log, both observed in
production).

For that reason `/scratchpad` can show and clear the note but not set it. If you
want standing text of your own, that is `/constraints`, which is injected
separately and framed as coming from you.

### Commands

```
/scratchpad          # show the current note (same as /scratchpad show)
/scratchpad clear    # discard it; gone from the next request
```

Reading the note mid-turn is allowed and is often the point — it shows what the
model is currently holding on to. Clearing it mid-turn is not.

### Turning it off

`sagittarius.scratchpadEnabled` (default **true**, global or project scope, live
toggle in `/settings`). Off means the tool is not registered and the prompt does
not mention it, so the model cannot call a tool that is not there.

Turn it off if you run short tasks that never approach the context window, or if
you are paying per token on a model that never needed it. A full note costs
about 1,000 tokens on **every** request.

## Session search

`search_session` takes a literal, case-insensitive substring and returns the
most recent matches from this session's transcript, newest first, each with a
turn number, role, timestamp, and a window of surrounding text.

```
query        required. Literal text, not a regular expression.
role         "user" | "model" | "any"   (default any)
max_results  default 20, hard cap 50
```

It searches your messages and the model's, **including tool-call arguments and
results** — the detail worth recovering is often a path that only ever appeared
as a `write_file` argument.

Scope is this session only. Cross-session search overlaps MemPalace and would
need a path-confinement story of its own; it is deliberately not here.

The tool is read-only and always registered. It has no toggle: it costs one tool
declaration and reads a file the session is already writing.

If session recording is disabled, the tool says so rather than returning an
empty result — "nothing matched" and "there is nothing to search" are very
different answers.

## Availability by mode

Both tools are read-only and available in every mode, including `ask`, `plan`,
`/grill`, and the `/readonly` inspection posture. Neither touches anything
outside session state you can inspect with `/scratchpad` and wipe with
`/scratchpad clear`.

`update_scratchpad` is deliberately **not** confirmation-gated, unlike
`save_memory`. `save_memory` rewrites `AGENTS.md` on disk permanently; the
scratchpad does not. Prompting for it would make it unusable on exactly the long
unattended tasks it exists for.

Subagents get neither tool. A subagent is a context firewall with its own
recorder and a short life: handing it the parent's scratchpad would leak the
context the firewall exists to contain, and it could not persist a note anyway.

## Overhead

| Cost | Scratchpad | Session search |
|------|-----------|----------------|
| Tokens, idle | none when empty | ~60, one tool declaration |
| Tokens, in use | up to ~1,000 per request (4,096-character cap) | the capped result, once per call |
| Latency | none; in-memory state and a string concat | one streaming pass, roughly 50–200 ms on a large session, disk-bound |
| Memory | 4 KB | read buffer plus at most 50 capped excerpts, under ~64 KB |
| Disk | one appended JSONL line per update; 50 rewrites in a session adds ~200 KB | none |

Neither builds an index, starts a watcher, or runs a background goroutine. A
session file is append-only and searched a handful of times per conversation, so
an index would cost more to maintain than the scan costs to run.

The per-request token cost of a populated scratchpad is the real expense here,
and the only reason the setting exists.

## Persistence and `--resume`

The note is written to the session JSONL as a metadata update on every change,
so `--resume` restores it and the first turn after the restart has it in the
system prompt. An explicit clear persists as a clear rather than as "this update
did not mention the scratchpad", so a resumed session cannot resurrect a note you
deliberately dropped.
