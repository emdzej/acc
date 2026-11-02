# Agent integrations

## Claude Code

Supported, and tested against Claude Code 2.1.x. `acc setup` installs:

**Hooks**, each running `acc hook claude-code`:

| Event | Used for |
|---|---|
| `SessionStart` | registering the session (does not deliver messages: no turn follows it, e.g. after `/compact`) |
| `UserPromptSubmit` | status *working*; clears attention; delivers messages |
| `PreToolUse` | current tool; `AskUserQuestion`/`ExitPlanMode` → *question* |
| `PostToolUse` | tool and output for the "Latest" view; delivers messages |
| `PostToolUseFailure` | failed tool output |
| `PermissionRequest` | *needs permission* badge |
| `Notification` | permission, idle and other notices |
| `Stop` | turn finished → *awaiting input*; delivers messages by blocking the stop |
| `SubagentStart` / `SubagentStop` | subagent tree, results and transcripts |
| `SessionEnd` | status *ended* |

Tool events use the matcher `*`. Each hook has a 5-second timeout in Claude
Code's settings; ACC itself gives up after 2 seconds.

**Status line**: `acc statusline`, which receives `rate_limits` (5-hour and
weekly windows) and `context_window` from Claude Code. If you already had a
`statusLine`, setup changes it to `acc statusline --chain '<your command>'`,
and `--uninstall` puts the original back.

**Message channel**: `acc mcp`, registered as a user-scope MCP server named
`acc` with `claude mcp add-json --scope user` (Claude Code rewrites
`~/.claude.json` itself, so ACC doesn't edit it). It declares the
`claude/channel` capability; sessions started with
`--dangerously-load-development-channels server:acc` (or `acc claude`) accept
its pushes. The server finds its session through Claude Code's
`~/.claude/sessions/<pid>.json` registry, and only claims messages if the
session's command line enables the channel. `--no-channel` skips it,
`--uninstall` removes it. See [Messaging](/guide/messaging).

### Subagents

Events fired inside a subagent carry the parent's `session_id` plus
`agent_id` and `agent_type`. ACC records them against the subagent, and they
never collect messages. `SubagentStop` provides the subagent's transcript
path and final message.

### What setup guarantees

- **Merge, never overwrite.** Only entries whose command ends in
  ` hook claude-code` (or ACC's status line) are added, replaced or removed.
  Your other hooks keep their exact bytes and key order.
- **Idempotent.** A second run reports *Nothing to do*.
- **Backed up and atomic.** The original is saved as
  `*.acc-bak-<timestamp>`, then the new file is written to a temporary file
  and renamed into place.
- **Absolute binary path** in commands, because hooks run with a minimal
  `PATH`. Setup refuses to write the path of a temporary `go run` binary.
- **No secrets** in agent config.

## OpenCode, Codex and others

Planned; see the [roadmap](/docs/roadmap). Both expose what ACC needs:
OpenCode through its plugin API (possibly with true push messaging) and its
SQLite store, which has token and cost data, and Codex through MCP, its hooks
and session logs with token counts. The adapter interface (`Detect`, `Plan`,
`PlanUninstall`) is agent-neutral.
