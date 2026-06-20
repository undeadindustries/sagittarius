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

Entries use service name `gemini-cli-provider-<providerId>` and account
`<providerId>`, compatible with the gemini-cli fork so keys can be shared
between tools on the same machine.

**Requirements on Linux:** `libsecret` and a running Secret Service (D-Bus).
Headless servers, WSL without a keyring, SSH sessions, and containers often
lack Secret Service. In those cases Sagittarius falls back automatically unless
you force file storage (below).

### Encrypted file fallback

When the OS keychain is unavailable—or when `GEMINI_FORCE_FILE_STORAGE=true`—
keys are stored in `~/.gemini/gemini-credentials.json` (or
`$GEMINI_CLI_HOME/.gemini/gemini-credentials.json`).

The file is encrypted with AES-256-GCM. The encryption key is derived via
scrypt from the machine hostname and username (same scheme as gemini-cli
FileKeychain). File mode is `0600`; directory mode is `0700`.

**Tradeoffs:**

| | Keychain | Encrypted file |
|---|----------|----------------|
| Protection | OS-isolated secret store, often unlock-gated | File permissions + host-bound key derivation |
| Multi-user | Per-user OS vault | Per-user file under home directory |
| Headless Linux | Often unavailable | Works without D-Bus/keyring |
| Backup/sync | Usually excluded from cloud backup | File may be copied with home-dir backups |
| Compromise model | Requires OS session / keyring unlock | Requires read access to home dir **and** same hostname/username context for scrypt salt |

Set `GEMINI_FORCE_FILE_STORAGE=true` when you deliberately want file storage
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
`~/.gemini/settings.json`. The config loader strips and rejects such fields.
