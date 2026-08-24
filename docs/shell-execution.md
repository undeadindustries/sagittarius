# Shell Execution in Sagittarius

Sagittarius implements a robust, feature-complete shell execution system designed for agentic AI usage, reaching parity with and exceeding `gemini-cli`.

## Features

1. **PTY Emulation & Headless VT**
   Every shell command executed by the `run_shell_command` tool is spawned inside a Pseudo-Terminal (PTY) using `github.com/creack/pty`.
   Its output is pumped through a headless VT emulator (`github.com/charmbracelet/x/vt`), providing accurate formatting, color-stripping, and handling of interactive console updates (like progress bars and carriage returns).

2. **Live Output Streaming**
   Commands stream their output live to the TUI. You don't have to wait for a 30-second task to finish to see what it is doing; the TUI updates in-place dynamically.

3. **Background Process Management**
   - **Auto-backgrounding**: Any foreground command that exceeds a threshold (default 30 seconds) is automatically detached and moved to the background. This ensures the AI turn is never blocked indefinitely by a server process (e.g. `python3 -m http.server`).
   - **Explicit backgrounding**: The model can explicitly request `is_background=true` for known servers.
   - **`&` Child Capture**: If a command spawns a background child using `&` (e.g. `sleep 30 &`), a shell trap `jobs -p` captures the child's PID and registers it.

4. **Background Process Viewer (Ctrl+B)**
   Pressing `Ctrl+B` opens an interactive background process viewer overlay, allowing you to list all tracked background processes, view their real-time uptime and status, read their log files, and selectively kill them.

5. **Spill files for large output**
   Completed command output larger than 64 KiB is truncated to a head+tail preview before it is
   sent to the model. When the spill directory can be written, the full text lands at
   `~/.sagittarius/tmp/<slug>/spill/sagittarius-spill-*.log` (mode `0600`) and the preview names
   that path so the model can page it with `read_file` (`start_line` / `end_line`). The tool
   result includes `spill_file`, `truncated`, and `total_lines`. `read_file` accepts that
   absolute path only when it is a regular file whose parent is exactly the configured spill
   directory and whose name matches `sagittarius-spill-*.log` (symlinks and other out-of-root
   paths stay rejected). If the spill directory is missing or the write fails, the preview is
   still truncated and says no spill file was written — there is no `spill_file` key, and the
   model is told to re-run with narrower arguments. Backgrounded commands keep using their live
   `log_file`; only the echoed "output so far" is capped, and that marker names the log rather
   than a spill file. The same spill pattern applies to large `grep_search` and `find_symbol`
   result text.

## Safety & Process Groups

Commands are spawned in their own Process Group (`Setpgid: true` implicitly via `pty.Start`). When a command is canceled (via `Esc`), a `SIGKILL` is dispatched to the entire process group (`-pid`), ensuring that no orphaned children or zombie processes are left behind.
