# Trajectory Analysis Guide

Sagittarius provides an ATIF (Agent Trajectory Interchange Format) analyzer to evaluate operational efficiency, tool health, coding hygiene, loop hazards, and terminal execution patterns.

## Commands

```bash
# Analyze an exported ATIF file or session ID
sagittarius --analyze-trajectory trajectory.json
sagittarius --analyze-trajectory latest

# Output as structured JSON
sagittarius --analyze-trajectory trajectory.json --output-format json

# Compare two trajectories (e.g. baseline vs candidate, or two different models)
sagittarius --analyze-trajectory baseline.json --compare candidate.json
```

---

## Metric Categories and Findings Remedies

### 1. Efficiency
- **Metric indicators:** Steps per user turn, LLM call count, token distribution (prompt, completion, cached, reasoning), and cache hit ratio.
- **Finding: High Steps Per Turn**
  - *Remedy:* Review mode prompts and tool declarations to ensure concise tool calling. Enable batch tool execution (`sagittarius.scriptToolEnabled: true`) where applicable.
- **Finding: Low Cache Hit Ratio**
  - *Remedy:* Keep system instructions stable and avoid mid-session system prompt mutations. Check provider caching compatibility.

### 2. Tool Health & Error Codes
- **Metric indicators:** Tool failure rate and histogram of error codes (`INVALID_ARGS`, `MODE_RESTRICTION`, `USER_DENIED`, `HOOK_DENIED`).
- **Finding: `INVALID_ARGS`**
  - *Remedy:* Inspect tool schema definitions or extend the tool alias normalizer (`internal/tools/args.go`) to remap alternate parameter names used by specific models.
- **Finding: `MODE_RESTRICTION` or `USER_DENIED`**
  - *Remedy:* When running in read-only inspection posture or ask/plan modes, ensure mutating actions are not attempted.

### 3. Loop Hazards
- **Finding: Redundant File Reads**
  - *Condition:* The same file is read twice across turns without any intermediate write or modification.
  - *Remedy:* Rely on previous read context or store key file facts in the session scratchpad (`update_scratchpad`) to avoid burning tokens on duplicate reads.
- **Finding: Identical Tool Call Repeated in Step**
  - *Condition:* Tool called multiple times with exact identical arguments within a single turn.
  - *Remedy:* Verify tool declaration parameters and temperature settings.
- **Finding: Repeated Edit Failures**
  - *Condition:* The `edit` tool failed multiple times on the same target file.
  - *Remedy:* Re-read the file to ensure `old_string` matches exact indentation and whitespace, or fall back to targeted full-file write when necessary.

### 4. Coding Hygiene
- **Finding: File Modifications Concluded Without Project Checks**
  - *Condition:* Changes made via `write_file` or `edit` but no verification (`run_project_checks` or build/test shell command) executed before the turn settled.
  - *Remedy:* Activate the `verify-after-edit` skill or configure `sagittarius.verify.suggestAfterWrite: true`.
- **Finding: Heavy Full File Rewrites Instead of Edits**
  - *Condition:* Multiple `write_file` calls on existing files with zero `edit` calls.
  - *Remedy:* Prefer the `edit` tool for surgical adjustments to preserve file structure and minimize token usage.

### 5. Terminal Patterns
- **Finding: High Non-Zero Shell Command Exits**
  - *Condition:* More than 30% of shell commands exited with non-zero status.
  - *Remedy:* Check shell command arguments, missing dependencies, or path assumptions in prompts.

### 6. Context Management & Subagents
- **Finding: Context Compaction / Truncation Triggered**
  - *Condition:* Session history exceeded model context budget.
  - *Remedy:* Delegate complex or verbose exploratory tasks to subagents (`task` or `code_task`) to isolate large search results from the parent context.
