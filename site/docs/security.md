# Security

ACC can send instructions into agents that run commands on your machine, so
it treats access to its API as sensitive.

## Local mode (default)

- The daemon listens on **loopback only** (`127.0.0.1:7317`) unless you
  [open it to the network](#network-access) and confirm.
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
- **Cross-site requests are refused.** A web page you visit can't post to
  the daemon: writes carrying another site's `Origin` (or
  `Sec-Fetch-Site: cross-site`) get `403`.
- **Unknown host names are refused** (`421`), so DNS rebinding can't make a
  page's own origin point at the daemon. IP addresses, `localhost` and this
  machine's name are accepted; add others with `--allow-host`.

## Network access

To use the dashboard from your phone or another computer, listen on all
interfaces:

```sh
acc serve --addr 0.0.0.0:7317                                    # sign in with a key
acc serve --addr 0.0.0.0:7317 --tls-cert cert.pem --tls-key key.pem
acc serve --addr 0.0.0.0:7317 --auth none                        # no sign-in
```

Before it listens, ACC shows the addresses it will be reachable at, the auth
and transport, and what access means, and asks you to type `yes` (`no auth`
for `--auth none`). Without a terminal, for example under a service manager,
pass `--yes`.

Whoever gets in can read your sessions and transcripts, send prompts your
agents act on, and [start](/guide/launching) or [resume](/guide/history)
sessions on this machine. Treat access as running code as you.

| `--auth` | Who gets in |
|---|---|
| `key` (default) | Holders of the **remote key**: a random 256-bit key in `~/.acc/remote_key` (mode 0600), or `$ACC_REMOTE_KEY`. Browsers get a sign-in page and an `HttpOnly`, `SameSite=Strict` cookie; API clients send it as a bearer token (`ACC_TOKEN=<key>`). The local token keeps working. Ten failed sign-ins per minute from one address get `429`. To rotate the key, delete the file and restart. |
| `none` | Anyone who can reach the port. Only on a network you fully trust. The cross-site and host-name checks still apply. |

**Transport.** Without TLS, the key and everything the dashboard shows cross
the network in plain HTTP. With `--tls-cert` and `--tls-key` (PEM; for
example from `mkcert` or `tailscale cert`), other machines must use HTTPS,
while this machine's hooks, CLI and TUI keep using plain
`http://127.0.0.1:7317` on the same port, so nothing else changes.

The `acc tui` embedded server always stays on loopback.
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

Until then, [network access](#network-access) is meant for networks you
trust (your LAN, a VPN such as Tailscale), not the public internet.
