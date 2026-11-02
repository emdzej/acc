# Security

ACC can send instructions into agents that run commands on your machine, so
it treats access to its API as sensitive.

## Local mode (today)

- The daemon listens on **loopback only** (`127.0.0.1:7317`) and refuses
  non-loopback addresses.
- Every API call needs the local **bearer token** in `~/.acc/token`
  (random, 256-bit, mode 0600). Comparisons run in constant time.
- The browser signs in once through `acc open`, which passes the token in the
  URL. The daemon swaps it for an `HttpOnly`, `SameSite=Strict` cookie and
  redirects so the token leaves the address bar.
- Transcript reads are restricted to `.jsonl` files under
  `~/.claude/projects` (or `$CLAUDE_CONFIG_DIR/projects`), after resolving
  symlinks. Paths come from hook payloads, so the API is never a general file
  reader.
- Every queued message is written to the daemon log with its target and sender.

## What ACC stores

Hook payloads are stored as they arrive, capped at 64 KB per event. They can
include your prompts, tool inputs and tool outputs. The database is local
(`~/.acc/acc.db`). Keep this in mind before hosting ACC.

## Hosted mode (planned)

A hosted ACC will sit behind **OIDC**:

- browser: Authorization Code + PKCE, with server-side sessions;
- TUI and CLI: Device Authorization Grant (`acc login`), with tokens in the OS
  keychain;
- agent hooks: short-lived, per-machine ACC tokens linked to your OIDC
  identity, revocable one machine at a time;
- roles from IdP group claims: *viewer* (read), *operator* (send messages),
  *admin*;
- redaction of hook payloads before they leave the machine.

Until then the daemon will not bind to a public address.
