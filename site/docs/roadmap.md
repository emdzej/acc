# Roadmap

## Done

- Claude Code integration: hooks, status line, `acc setup` / `--uninstall` / `doctor`
- Daemon with SQLite, live SSE stream, spool and replay
- Full-screen TUI (with an embedded server) and a web dashboard (dark, light, auto)
- Group tree with inherited colours; per-session colours
- Messaging sessions and groups through hooks
- Subagents nested under sessions
- Attention badges (permission, question, notice, input)
- Latest-activity view and opt-in live transcript
- `/rename` titles, usage and cost, context fill, plan limits

## Next

- **OpenCode** adapter: plugin-based events, push messaging if the SDK allows,
  usage from its SQLite store
- **Codex** adapter: MCP and session logs, token usage
- **MCP server** (`acc mcp`) so agents can report status and read their inbox
- **Claude Code channels**: push messages into idle sessions
- **macOS menu-bar app**: the "need you" count, native notifications, the
  dashboard in a native window
- **Hosted mode** behind OIDC, with per-machine tokens and roles
- Prebuilt releases (Homebrew, GitHub Releases)
