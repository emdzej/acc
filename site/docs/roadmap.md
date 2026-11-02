# Roadmap

## Done

- Claude Code integration: hooks, status line, `acc setup` / `--uninstall` / `doctor`
- Daemon with SQLite, live SSE stream, spool and replay
- Full-screen TUI (with an embedded server) and a web dashboard (dark, light, auto)
- Group tree with inherited colours; per-session colours
- Messaging sessions and groups: instant over a Claude Code channel (`acc mcp`, wakes idle sessions), else through hooks
- Starting sessions from ACC in Ghostty, Terminal, iTerm or tmux (`acc new`, `N`, **+ Session**)
- ACC-side session titles (`acc rename`, `r`)
- Subagents nested under sessions
- Attention badges (permission, question, notice, input)
- Latest-activity view and opt-in live transcript
- `/rename` titles, usage and cost, context fill, plan limits

## Next

- **OpenCode** adapter: plugin-based events, push messaging if the SDK allows,
  usage from its SQLite store
- **Codex** adapter: MCP and session logs, token usage
- **Slash commands** (`/rename`, `/compact`…) in sessions: Ghostty's scripting can type into a terminal; channel prompts can't run them
- **Two-way channel**: a reply tool on `acc mcp` so agents can answer ACC and report status
- **Channel permission relay**: answer Claude Code permission prompts from the TUI and web UI
- **macOS menu-bar app**: the "need you" count, native notifications, the
  dashboard in a native window
- **Hosted mode** behind OIDC, with per-machine tokens and roles
- Homebrew tap
