# Reporting Security Issues

If you discover a security vulnerability in Sagittarius, please report it
responsibly rather than opening a public issue with exploit details.

## Preferred reporting

1. Open a **private security advisory** on GitHub:
   **Repository → Security → Advisories → Report a vulnerability**

2. If GitHub advisories are unavailable, email the maintainers with:
   - Description of the issue and potential impact
   - Steps to reproduce
   - Affected versions or commits
   - Any suggested fix (optional)

## What to expect

- Acknowledgment within **5 business days**
- Coordinated disclosure — we will work with you on timing and credit
- A fix or mitigation plan before public disclosure when possible

## Out of scope

- Issues in third-party dependencies already tracked upstream
- Social engineering or physical attacks
- Denial-of-service against public infrastructure without a reproducible local PoC

## Safe harbor

We support good-faith security research that follows this policy and avoids
privacy violations, data destruction, or service disruption.

---

## Credential storage threat model

Sagittarius stores provider API keys outside `settings.json` (see AD-005).
Resolution order is: **environment variable → secure storage → error**.

### OS keychain (preferred)

When available, API keys are stored in the platform credential manager:

| Platform | Backend |
|----------|---------|
| Linux | Freedesktop Secret Service (e.g. GNOME Keyring, KWallet via libsecret) |
| macOS | Keychain |
| Windows | Credential Manager |

Entries use service name `sagittarius-provider-<providerId>` and account
`<providerId>`. The layout follows the gemini-cli fork's scheme, but the
Sagittarius service prefix is distinct, so keys are not shared with gemini-cli.

**Requirements on Linux:** `libsecret` and a running Secret Service (D-Bus).
Headless servers, WSL without a keyring, SSH sessions, and containers often
lack Secret Service. In those cases Sagittarius falls back automatically unless
you force file storage (below).

### Encrypted file fallback

When the OS keychain is unavailable—or when `SAGITTARIUS_FORCE_FILE_STORAGE=true`—
keys are stored in `~/.sagittarius/sagittarius-credentials.json` (or
`$SAGITTARIUS_HOME/.sagittarius/sagittarius-credentials.json`).

The file is encrypted with AES-256-GCM. The encryption key is derived via
scrypt from the machine hostname and username (the encryption format matches the
gemini-cli FileKeychain, but the scrypt salt/password are Sagittarius-specific,
so the file is not interchangeable with gemini-cli). File mode is `0600`;
directory mode is `0700`.

**Tradeoffs:**

| | Keychain | Encrypted file |
|---|----------|----------------|
| Protection | OS-isolated secret store, often unlock-gated | File permissions + host-bound key derivation |
| Multi-user | Per-user OS vault | Per-user file under home directory |
| Headless Linux | Often unavailable | Works without D-Bus/keyring |
| Backup/sync | Usually excluded from cloud backup | File may be copied with home-dir backups |
| Compromise model | Requires OS session / keyring unlock | Requires read access to home dir **and** same hostname/username context for scrypt salt |

Set `SAGITTARIUS_FORCE_FILE_STORAGE=true` when you deliberately want file storage
(e.g. CI, minimal containers) and accept the weaker isolation compared to a
system keychain.

### Environment variables

`GEMINI_API_KEY`, `GOOGLE_API_KEY`, and provider-specific vars (e.g.
`OPENAI_API_KEY`) override stored keys. Env vars are visible to all processes
in the same user session and may appear in shell history or process listings.
Prefer the keychain for interactive use; use env vars for automation when the
host environment is already trusted.

### Logging

Sagittarius does not log API key values. Debug output uses redacted placeholders.

### What we do not store in settings.json

API keys, bearer tokens, and other secrets must not appear in
`~/.sagittarius/settings.json`. The config loader strips and rejects such fields.

---

## Project boundary enforcement

By default the built-in file tools confine reads and writes to the workspace
root (the directory Sagittarius was launched in). Shell commands, however, can
still mutate paths anywhere the user can. The optional **project boundary**
hardens this: when enabled, Sagittarius blocks mutating operations whose target
resolves outside the project root.

Enable it globally or per project in `settings.json` (project wins):

```json
{
  "security": {
    "projectBoundary": {
      "enforce": true
    }
  }
}
```

When `enforce` is `true`:

| Tool | Behavior |
|------|----------|
| `write_file` | Blocked when the resolved path is outside the project root. |
| `read_file`, `list_directory`, `grep_search` | Unaffected — the boundary targets mutations, not reads. |
| `run_shell_command` | Blocked when a heuristic scan finds an out-of-project write/delete. |

The shell heuristic inspects the command string for output redirections
(`>`, `>>`, `2>`, `&>`, `>|`, `tee`) and known mutators (`rm`, `rmdir`, `mv`,
`cp`, `install`, `truncate`, `chmod`, `chown`, `mkdir`, `dd`, `ln`, `touch`,
`sed -i`) whose path arguments resolve outside the root (absolute paths, `../`
escapes, or `~` home references).

### Heuristic limitations

The scan is conservative and operates on the literal command string. It **cannot**
catch every escape, including:

- Obfuscation: `eval`, base64-decoded commands, or variable indirection
  (`f=/etc/x; rm "$f"`).
- A `cd` into another directory before a relative-path mutation.
- Commands run through a subshell or interpreter (`python -c '...'`,
  `bash -c '...'`).

Treat the boundary as defense-in-depth that stops common accidental escapes,
**not** as a sandbox. For stronger isolation run Sagittarius inside a container
or restricted user account. The protected-path guard for Sagittarius snapshot
metadata under `<repo>/.sagittarius/snapshots/` is always active, independent of
this flag.

A separate, related feature records local file changes for review and rollback;
see [docs/snapshots-and-undo.md](docs/snapshots-and-undo.md).
