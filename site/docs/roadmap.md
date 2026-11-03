# Roadmap

## Done

- Claude Code integration: hooks, status line, `acc setup` / `--uninstall` / `doctor`
- Daemon with SQLite, live SSE stream, spool and replay
- Full-screen TUI (with an embedded server) and a web dashboard (dark, light, auto)
- Group tree with inherited colours; per-session colours
- Messaging sessions and groups: instant over a Claude Code channel (`acc mcp`, wakes idle sessions), else through hooks
- Starting sessions from ACC in Ghostty, Terminal, iTerm or tmux (`acc new`, `N`, **+ Session**)
- ACC-side session titles (`acc rename`, `r`)
- tmux hosting for started sessions (`--tmux`), bring back (`acc attach`, `a`)
- Slash commands typed into sessions ACC started (tmux or Ghostty)
- Subagents nested under sessions
- Attention badges (permission, question, notice, input)
- Latest-activity view and opt-in live transcript
- `/rename` titles, usage and cost, context fill, plan limits
- Plan pace forecast: a warning when the current pace hits a limit before it resets
- One tree for groups and sessions; group completion when assigning
- Session colours on the session panel and the tmux status bar
- Background subagents keep their session *working*
- **OpenCode** adapter: plugin events, instant messages that wake idle sessions, slash commands, usage, titles
- Session goals (`/goal`) for Claude Code and OpenCode
- **pi** adapter: extension events, instant messages that wake idle sessions, usage, History, starting and resuming sessions

## Next

- **OpenCode**: live transcript view, child sessions nested like subagents, starting OpenCode sessions from ACC
- **Codex, Copilot CLI and Gemini CLI** adapters: hooks, permission prompts, History, resume and starting sessions, usage, and plan limits for Codex. See [Adding an agent](/docs/adding-an-agent)
- **pi**: live transcript view
- **Slash commands in sessions ACC didn't start**: match a Ghostty terminal to its session reliably
- **Per-group Claude accounts** (`CLAUDE_CONFIG_DIR` per group), with hooks installed into each account
- **Two-way channel**: a reply tool on `acc mcp` so agents can answer ACC and report status
- **Channel permission relay**: answer Claude Code permission prompts from the TUI and web UI
- **macOS menu-bar app**: the "need you" count, native notifications, the
  dashboard in a native window
- **Hosted mode** behind OIDC, with per-machine tokens and roles
- Homebrew tap
