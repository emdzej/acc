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

ACC also reads each session's transcript for what hooks don't carry:
`/rename` and auto-generated titles, `/goal` records, usage (`cost-state`)
and the live transcript view.

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

## OpenCode

Supported, and tested against OpenCode 2.2. `acc setup` (or
`acc setup --agent opencode`) installs one file, the ACC plugin:

| Scope | Path |
|---|---|
| user (default) | `~/.config/opencode/plugins/acc.js` (or under `$XDG_CONFIG_HOME`) |
| project | `.opencode/plugins/acc.js` in `--project-dir` |

The plugin starts with a `// acc-managed` marker. Setup only overwrites or
removes a file with that marker and refuses to touch any other `acc.js`.
It records the ACC state directory and server URL at install time, and
`ACC_HOME`, `ACC_SERVER` and `ACC_TOKEN` override them at run time.

**Events.** The plugin subscribes to OpenCode's event bus and forwards the
session events ACC understands to `POST /api/v1/agents/opencode/events`.
ACC translates them in Go, so changes to the mapping don't need a plugin
reinstall:

| OpenCode event | ACC |
|---|---|
| `session.created` | session start, working directory |
| `session.inbox.enqueued`, `session.execution.started` | prompt: *working* |
| `session.step.started` | model in use |
| `session.tool.called` / `.success` / `.failed` | current tool; tool output for the Latest view |
| `session.text.ended` | last reply |
| `session.execution.succeeded` / `.failed` / `.interrupted` | turn finished: *awaiting input*, with the reply or the error |
| `permission.asked` / `.replied` | *needs permission* badge, with the permission and its patterns |
| `form.created` / `.replied` / `.cancelled` | *question* badge |
| `session.renamed` | title |
| `session.usage.updated` | cost and tokens |
| `session.deleted`, `session.archived` | *ended* |
| goal changes (`opencode.goal`) | the session's [goal](/guide/monitoring#goals) |

**Messages.** The plugin follows ACC's live stream, with a 20-second
heartbeat as a fallback, and claims messages for the sessions it hosts.
Prompts go to `session.prompt`, which wakes an idle session; `/commands` go
to `session.command`. It only claims messages for sessions its OpenCode
instance can open, so a message is never claimed by the wrong process.

Everything is best-effort: when ACC isn't running, OpenCode carries on,
events from that time are dropped, and the plugin reconnects in the
background. OpenCode loads plugins when it
starts, so restart it (and its background service, if you use one) after
installing.

What's not covered yet: the live transcript view, plan limits (OpenCode
doesn't report them) and child sessions, which show as separate sessions.

## pi

Supported, and tested against pi 1.5 (`@earendil-works/pi-coding-agent`).
`acc setup` (or `acc setup --agent pi`) installs one file, the ACC
extension:

| Scope | Path |
|---|---|
| user (default) | `~/.pi/agent/extensions/acc.js` (or under `$PI_CODING_AGENT_DIR`) |
| project | `.pi/extensions/acc.js` in `--project-dir`; pi loads it once you trust the project |

Like the OpenCode plugin, it starts with a `// acc-managed` marker, records
the ACC state directory and server URL at install time (`ACC_HOME`,
`ACC_SERVER` and `ACC_TOKEN` override them at run time), and is
best-effort: when ACC isn't running, pi carries on.

**Events.** The extension forwards pi's events, in order, to
`POST /api/v1/agents/pi/events`, where ACC translates them (`internal/pi`):

| pi event | ACC |
|---|---|
| `session_start` | session start: directory, session file, name |
| `input` | prompt: *working* |
| `tool_execution_start` / `_end` | current tool; tool output for the Latest view; failures |
| `ui_prompt_start` / `_end` | a dialog from an extension: *needs permission* for a confirm (a permission gate), *question* for others |
| `agent_settled` | turn finished: *awaiting input*, with the last reply and context use |
| `session_info_changed` | the session's name, as its title |
| `model_select` | model in use |
| `session_shutdown` | *ended* (not on `/reload`, which keeps the session) |

pi asks for no approvals itself, so *needs permission* only appears when an
extension asks, for example pi's `permission-gate` example. ACC uses
`agent_settled` for the end of a turn, not `agent_end`, because retries and
queued messages can follow `agent_end`.

**Usage** comes from the session file: pi records tokens and cost with
every response, and ACC adds them up, including abandoned branches of the
session tree and cache warming. **History** reads the same files.

**Messages.** The extension follows ACC's live stream, with a 20-second
heartbeat, and claims messages for its own session. Prompts go to
`pi.sendUserMessage`, as steering messages while pi is busy; a single-line
`/command` runs extension commands, skills and prompt templates. Print
(`-p`) and JSON runs report their events but don't take messages.

**Starting and resuming**: `pi --session-id <id> --name <title> -- <prompt>`
and `pi --session <id>`.

What's not covered yet: the live transcript view, and spotting a pi session
whose process died without shutting down (pi has no registry of running
sessions; forget the session to clear it).

## Codex

Supported, and tested against Codex CLI 0.166. `acc setup` (or
`acc setup --agent codex`) merges ACC's hooks into Codex's hooks file:

| Scope | Path |
|---|---|
| user (default) | `~/.codex/hooks.json` (or under `$CODEX_HOME`) |
| project | `.codex/hooks.json` in `--project-dir`; Codex loads it once the project is trusted |

Each hook runs `acc hook codex`. Like Claude Code's, setup only adds,
replaces or removes entries whose command ends in ` hook codex`, and a
second run changes nothing.

::: warning Trust the hooks once
Codex runs a new or changed hook only after you review and trust it: open
Codex and use `/hooks`. Trust is recorded per hook, by its content, so
re-running setup keeps it; moving the `acc` binary to another path needs a
new review.
:::

**Hooks.** Codex's hook events carry the same names and meaning as Claude
Code's, so they need little translation:

| Event | Used for |
|---|---|
| `SessionStart` | registering the session (Codex sends it with the first prompt, also after a resume) |
| `UserPromptSubmit` | status *working*; delivers messages |
| `PreToolUse` / `PostToolUse` | current tool and command; `PostToolUse` delivers messages |
| `PermissionRequest` | *needs permission* badge, with the command |
| `Stop` | turn finished → *awaiting input*; delivers messages by blocking the stop, so Codex continues with them |
| `Interrupt` | you stopped the turn: *awaiting input* |
| `SubagentStart` / `SubagentStop` | subagent tree |
| `PreCompact` | compaction |
| `SessionEnd` | status *ended* |

**From the rollout** (the session file the hooks name) ACC reads usage
(tokens, with cached input separately; Codex records no cost), context use,
the model, and the account's **plan limits**: the 5-hour and weekly windows
with their resets, which appear in the status bar and get the same
[forecast](/guide/monitoring#will-you-hit-the-limit) as Claude Code's.

**History** comes from the `threads` table of Codex's state database
(`~/.codex/state_<n>.sqlite`, read-only): sessions with a prompt or a
name, without archived ones and subagents.

**Starting and resuming.** Codex picks its own session IDs, so ACC starts
`codex -- <prompt>` with `ACC_LAUNCH_ID` set to the ID it registered the
session under; the session's first hook claims that registration with its
title, group and colour, and the ID ACC printed keeps working as an alias.
`codex resume <id>` resumes. Codex reports a resumed session only at its
next prompt, so ACC shows it *awaiting input* until then.

What's not covered yet: the live transcript view, cost (Codex records
tokens, not dollars), and messages to a session idle at its prompt, which
wait until it next acts, as for Claude Code without a channel.

## Copilot CLI

Supported, and tested against GitHub Copilot CLI 1.0.105. `acc setup` (or
`acc setup --agent copilot`) writes ACC's own hooks file, next to any you
have:

| Scope | Path |
|---|---|
| user (default) | `~/.copilot/hooks/acc.json` (or under `$COPILOT_HOME`) |
| project | `.github/hooks/acc.json` in `--project-dir` |

Each hook runs `acc hook copilot`. Copilot loads every file in its hooks
directory, so ACC never edits yours; it refuses to overwrite an `acc.json`
holding hooks it didn't write. `--uninstall` removes the file.

**Hooks.** ACC registers Claude-style event names, for which Copilot sends
Claude-style payloads:

| Event | Used for |
|---|---|
| `SessionStart` | registering the session (Copilot sends it with the first prompt after a resume) |
| `UserPromptSubmit` | status *working*; delivers messages |
| `PreToolUse` / `PostToolUse` / `PostToolUseFailure` | current tool and command; `PostToolUse` delivers messages |
| `Notification` | *needs permission* (an approval prompt) or *question*; other notifications are ignored |
| `Stop` | turn finished → *awaiting input*, with the reply; delivers messages by blocking the stop, so Copilot takes them as its next prompt |
| `SubagentStop` | subagent finished |
| `PreCompact` | compaction |
| `ErrorOccurred` | an unrecoverable error: a *notice* badge |
| `SessionEnd` | status *ended* |

`PermissionRequest` is left out: Copilot fires it for every tool call,
approved automatically or not. The approval prompt itself arrives as a
`Notification`.

**From the session's event log** (`~/.copilot/session-state/<id>/events.jsonl`)
ACC reads usage (tokens per model; Copilot bills premium requests, so cost
shows only when Copilot records one), the model and the turn's last reply.
**History** reads each session's `workspace.yaml`: its directory, name and
dates.

**Starting and resuming.** ACC starts `copilot --session-id=<id> --name=<title> -i <prompt>`
and resumes with `copilot --resume=<id>`. Copilot reports a resumed
session only at its next prompt, so ACC shows it *awaiting input* until
then.

What's not covered yet: the live transcript view, plan limits, and
messages to a session idle at its prompt, which wait until it next acts.

## Gemini CLI

Supported, and tested against Gemini CLI 0.66. `acc setup` (or
`acc setup --agent gemini`) merges ACC's hooks into Gemini's settings:

| Scope | Path |
|---|---|
| user (default) | `~/.gemini/settings.json` (or under `$GEMINI_CLI_HOME`) |
| project | `.gemini/settings.json` in `--project-dir` |

Each hook runs `acc hook gemini`. Setup only adds, replaces or removes
entries whose command ends in ` hook gemini`; the rest of your settings is
kept byte for byte.

::: warning Trusted folders only
Gemini runs hooks only in folders you trust (it asks the first time, or
use `/permissions`). Sessions in other folders don't appear in ACC.
:::

**Hooks.** Gemini's events have their own names; ACC maps them:

| Event | Used for |
|---|---|
| `SessionStart` | registering the session (also on resume) |
| `BeforeAgent` | a prompt: status *working*; delivers messages |
| `BeforeTool` / `AfterTool` | current tool and command; `AfterTool` delivers messages |
| `Notification` | *needs permission*, with the command waiting for approval |
| `AfterAgent` | turn finished → *awaiting input*, with the reply; delivers messages by blocking, so Gemini runs them as the next prompt |
| `SessionEnd` | status *ended* |

`PreCompress` is left out: Gemini fires it before every model call.
A message delivered at the end of a turn shows in Gemini as a warning,
"Agent execution blocked: delivering a message from Agents Control
Centre", followed by Gemini's answer to it.

**From the chat recording** (`~/.gemini/tmp/<project>/chats/session-*.jsonl`)
ACC reads usage (tokens, with cached input separately; Gemini records no
cost) and the model. **History** lists the recordings, with each
project's directory and the session's summary or first prompt.

**Starting and resuming.** ACC starts `gemini --session-id <id> -i <prompt>`
(Gemini sessions have no name; the title stays in ACC) and resumes with
`gemini --resume <id>`, in the session's directory.

What's not covered yet: the live transcript view, plan limits, context
use, and messages to a session idle at its prompt.
