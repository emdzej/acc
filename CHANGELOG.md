# Changelog

## 0.1.0

First release of ACC, the Agents Control Centre: one binary (`acc`) with a
daemon, web dashboard, TUI and CLI for watching and steering Claude Code
sessions.

- **Monitoring**: sessions with nested subagents, status, latest tool call or
  message, opt-in live transcript, attention badges (permission, question,
  input), usage and cost, context fill, and plan limits from the status line.
- **Groups and colours**: a group tree of any depth with inherited colours,
  plus per-session colours.
- **Messaging**: send to a session or a whole group. Sessions started with
  ACC's channel (`acc claude`, or `--dangerously-load-development-channels
  server:acc`) receive messages instantly, even when idle; others receive them
  at their next hook.
- **Starting sessions**: `acc new`, `N` in the TUI or **+ Session** in the web
  UI opens a terminal (Ghostty, Terminal, iTerm, tmux or custom) running a new
  session, pre-grouped and titled.
- **Titles**: `/rename` and auto titles are picked up; `acc rename` sets an
  ACC-side title.
- **Setup**: `acc setup` merges hooks and the status line into Claude Code's
  settings (backed up, idempotent, reversible with `--uninstall`) and
  registers the `acc mcp` channel.
- **Dashboards**: full-screen TUI (embeds the server when none is running)
  and a web UI with dark and light themes.

Binaries for macOS and Linux (arm64, amd64). Local use only for now: the
daemon refuses non-loopback addresses until OIDC-protected hosted mode lands.
