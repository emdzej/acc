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
  `opencode --session <id>` for OpenCode. The session keeps its ID, so its groups, colour and
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
(`~/.claude/sessions/`).

## Ended, even without saying so

A Claude Code session whose process is gone is shown as **ended**, even if
it never said goodbye (a closed terminal, a crash, a reboot): ACC checks
Claude Code's registry of running sessions every 20 seconds. If the session
reports in again, for example after you resume it, it comes back to life.

## History

**History** lists every session the agents have on record, newest first,
whether or not ACC has seen it:

- **Claude Code**: every transcript in `~/.claude/projects/` (and
  `$CLAUDE_CONFIG_DIR`), titled with its `/rename` or generated title, or
  else its first prompt.
- **OpenCode**: the top-level sessions in its database
  (`~/.local/share/opencode/opencode.db`, read-only), with their cost.

Each entry says whether the session is **running** and whether it is **in
ACC**. From there you can:

- **resume** it in a new window (see above),
- **add** it to ACC without opening it, as an ended session you can group,
  colour, read the transcript of and resume later.

| Where | How |
|---|---|
| TUI | `H`, type to filter, `Enter` to pick: `Enter` again resumes, `a` adds it to ACC |
| Web | **History** above the tree: filter, then **↻ Resume**, **+ Add** or **Open** |
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
