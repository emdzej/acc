# Changelog

## Unreleased

- **OpenCode support**: `acc setup` installs an ACC plugin for OpenCode 2.x
  (user or project scope). Sessions report status, tools, permissions,
  questions, titles, model and usage; messages are delivered at once and wake
  idle sessions; `/commands` run in any OpenCode session.
- **Goals**: a session's `/goal` (Claude Code and OpenCode) shows in the
  session panel with its status, round and the judge's latest verdict.
- **Site**: every feature documented, screenshots of the TUI and the web UI
  made from fake sessions (`scripts/demo.sh` starts a demo daemon), and no
  emoji.
- `acc ls` shows 16 characters of OpenCode IDs; `acc setup` only prints
  hints for the agents it set up.
- Fixed: the web UI showed a raw template for queued messages; tool views no
  longer show an empty output for permission requests.
- **One tree**: groups and sessions share a single tree in the TUI and the
  web UI (groups, their subgroups and sessions, subagents; ungrouped
  sessions at the root). Keys act on the selected row.
- **Colours in more places**: the session panel's info block is tinted with
  the session's colour (TUI and web), and tmux-hosted sessions get their
  status bar in it, with the title, kept in sync as colours change.
- **Group completion in the TUI**: `g` and `u` suggest valid groups as you
  type (any part of the path), with `Tab` to complete and `↑↓` to choose, and
  refuse unknown paths. `N` suggests known session directories.
- **Plan pace forecast**: plan windows show **⚠** with the expected time when,
  at the recent pace, a limit runs out before it resets (tooltip in the web UI).
- **tmux hosting**: `acc new --tmux` (or `"tmux": true`, the TUI's last
  question, **Host** in the web form) runs the session in its own tmux
  session with your terminal attached, so it survives closed windows. Bring
  it back with `acc attach`, `a` in the TUI or **↗ bring back** in the web UI.
- **Slash commands**: a single-line `/command` sent to a session ACC started
  is typed into its terminal (tmux, or Ghostty via AppleScript). ACC refuses
  while a permission or question dialog is open.
- **Background subagents**: a session stays *working* after its turn ends
  while subagents still run, and turns idle when the last one stops.

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
