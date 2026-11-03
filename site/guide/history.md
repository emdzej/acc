# Resume, history and forget

Sessions come and go: a terminal gets closed, a laptop reboots, last week's
session is worth picking up again. ACC can bring sessions back, find every
session the agents have on record, and drop the ones you're done with.

## Resume a session

**Resume** brings a session back on screen:

- If ACC [started it](/guide/launching) and its window or tmux session is
  still there, ACC brings that window back.
- Otherwise ACC opens a new terminal window in the session's directory that
  continues it: `claude --resume <id>` for Claude Code,
  `opencode --session <id>` for OpenCode, `pi --session <id>` for pi,
  `codex resume <id>` for Codex, `copilot --resume=<id>` for Copilot CLI,
  `gemini --resume <id>` for Gemini CLI. The session keeps its ID, so its groups, colour and
  title in ACC stay as they were.

| Where | How |
|---|---|
| TUI | `a` on a session |
| Web | **↻ resume** in the session panel of an ended session (**↗ bring back** while its window is open) |
| CLI | `acc resume <session>` (`--tmux` to host it in tmux) |

The new window uses the same terminal, command and tmux settings as
[starting a session](/guide/launching#configuration); for OpenCode the
command is `launch.opencode_command` (default `opencode`).

ACC won't run a session twice. A Claude Code session still running in a
terminal ACC didn't open can't be resumed; switch to that window. ACC knows
which sessions are running from Claude Code's own registry of its processes
(`~/.claude/sessions/`). pi keeps no such registry, so ACC goes by what the
session last reported: a session of another agent that hasn't ended can't be
resumed. If its process is gone without saying so (a closed terminal or a
crash), [forget](#forget-sessions) it, then resume it from History. Sessions
ACC started in tmux are the exception: when their tmux session is gone, ACC
knows the agent is too.

## Ended, even without saying so

A Claude Code session whose process is gone is shown as **ended**, even if
it never said goodbye (a closed terminal, a crash, a reboot): ACC checks
Claude Code's registry of running sessions every 20 seconds. The same goes
for a session of any agent that ACC [started in tmux](/guide/launching)
once its tmux session is gone. If the session
reports in again, for example after you resume it, it comes back to life.

## History

**History** lists every session the agents have on record, newest first,
whether or not ACC has seen it:

- **Claude Code**: every transcript in `~/.claude/projects/` (and
  `$CLAUDE_CONFIG_DIR`), titled with its `/rename` or generated title, or
  else its first prompt.
- **OpenCode**: the top-level sessions in its database
  (`~/.local/share/opencode/opencode.db`, read-only), with their cost.
- **Codex**: the sessions in its state database (`~/.codex/state_<n>.sqlite`,
  read-only), titled with their name, title or first prompt.
- **Copilot CLI**: the sessions in `~/.copilot/session-state/` (or
  `$COPILOT_HOME`), titled with their name, with their tokens.
- **Gemini CLI**: the chat recordings in `~/.gemini/tmp/*/chats/`, titled
  with their summary or first prompt, with their tokens.
- **pi**: the session files in `~/.pi/agent/sessions/` (or
  `$PI_CODING_AGENT_SESSION_DIR`), titled with the session's name or else
  its first prompt, with their cost.

History opens as a dialog: a filter box, the list (● running in a terminal,
◆ in ACC), and the full details of the highlighted session: its title,
directory, agent and ID, when it started and was last active, its size or
usage, whether it is running or in ACC, and where its transcript is. Type
to filter (every word must match the title, directory, ID or agent), and
`↑↓` / `PgUp` `PgDn` to choose. From there you can:

- **resume** it in a new window (see above),
- **add** it to ACC without opening it, as an ended session you can group,
  colour, read the transcript of and resume later.

| Where | How |
|---|---|
| TUI | `H`, type to filter, `↑↓` to choose: `Enter` resumes it (or opens it if it is live), `Ctrl-A` adds it to ACC only |
| Web | **History** above the tree: filter, `↑↓` or click to choose, `Enter` or double-click resumes (or opens); **↻ Resume**, **+ Add to ACC** and **Open in ACC** under the details |
| CLI | `acc history [filter]`, then `acc resume <id>` or `acc history --add <id>` |

ACC only reads the agents' records; it never changes or deletes them.

## Forget sessions

**Forget** removes a session from ACC: its events, queued messages, group
memberships and colour. The agent keeps the session: History still lists it
and you can resume or add it again. A live session you forget reappears when
it next reports in.

| Where | One session | All ended sessions |
|---|---|---|
| TUI | `x` (or `Delete`), then `y` | `X`, then `y` |
| Web | **forget** in the session panel | **Forget ended sessions** in History |
| CLI | `acc forget <session>…` | `acc forget --ended` |

To only hide ended sessions for a while, toggle them with `e` in the TUI or
**hide ended** in the web UI.
