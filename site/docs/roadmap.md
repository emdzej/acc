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
- **Codex** adapter: hooks, permission prompts, messages, usage and plan limits from rollouts, History, starting and resuming sessions
- **Copilot CLI** and **Gemini CLI** adapters: hooks, permission prompts, messages, usage, History, starting and resuming sessions
- Agent registry: one place declares what ACC can do with each agent (start, resume, message delivery), served at `GET /api/v1/agents`; an agent picker for new sessions, `launch.commands` overrides. See [Adding an agent](/docs/adding-an-agent)
- History dialog in the TUI and web UI: every agent's sessions with their directory, dates, usage and state; resume, open or add to ACC
- Sessions ACC started in tmux are marked ended when their tmux session is gone, for every agent
- Fast catch-up on start: events spooled while ACC wasn't running replay in about a second, even thousands of them
- Plan limits named per agent when several report them (Claude Code and Codex)

## Next

- **OpenCode**: live transcript view, child sessions nested like subagents, starting OpenCode sessions from ACC
- **pi, Codex, Copilot CLI and Gemini CLI**: live transcript view
- **Codex, Copilot CLI and Gemini CLI**: messages that wake a session idle at its prompt (today they wait for its next hook, as for Claude Code without a channel); context use for Gemini CLI
- **Slash commands in sessions ACC didn't start**: match a Ghostty terminal to its session reliably
- **Per-group Claude accounts** (`CLAUDE_CONFIG_DIR` per group), with hooks installed into each account
- **Two-way channel**: a reply tool on `acc mcp` so agents can answer ACC and report status
- **Channel permission relay**: answer Claude Code permission prompts from the TUI and web UI
- **macOS menu-bar app**: the "need you" count, native notifications, the
  dashboard in a native window
- **Hosted mode** behind OIDC, with per-machine tokens and roles
- Homebrew tap
