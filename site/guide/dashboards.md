# Dashboards

The TUI and the web dashboard show the same data and offer the same actions.
Use whichever suits you; they update live and can run side by side.

## Terminal (TUI)

```sh
acc tui
```

<!-- screens/tui.png: ACC_DOCS_SIZE=150x36 ACC_DOCS_SCREEN=/tmp/s.ansi go test ./internal/tui -run DocsScreen, scripts/ansi2html.py, screenshot -->
![The ACC terminal dashboard: a tree of groups and sessions, and the open session with its goal, context, usage and a permission prompt](/screens/tui.png)

The screen has two panels:

- **Sessions** (left) is one tree: each group with its subgroups and
  sessions, then ungrouped sessions at the root. A session in several groups
  shows under each. `→` expands a session to show its subagents, `Enter` or
  `←`/`→` fold a group, and `←` on a closed row jumps to its parent.
- **Session** (right) shows the session you opened with `Enter`, or a
  subagent. Its info block is tinted with the session's colour, and shows the
  [goal](/guide/monitoring#goals) when one is set.

Keys act on the selected row: on a group, `m` messages all its live
sessions, `c` colours it and `d` deletes it; `n` and `N` create a subgroup or
a session in the group you're in. The bottom bar shows the shortcuts for the
selected row; `?` lists them all.

The status bar shows plan limits, e.g. `5h 80% ⚠15:39 ↻16:53`: 80% of the
5-hour window used, resetting at 16:53, and **⚠** marks that at the current
pace it runs out at 15:39, before the reset. See [Monitoring](/guide/monitoring#plan-limits).

| Key | Action |
|---|---|
| `Tab` / `Shift-Tab` | switch between the tree and the session panel |
| `↑↓` `j k` `PgUp/PgDn` | move or scroll |
| `/` or `Ctrl-F` | jump to a session: type part of its title, group, directory or ID, `↑↓` to choose, `Enter` opens it (unfolding its group) |
| `Enter` or click | open the session or subagent; fold a group |
| `→ ←` `Space` | expand or collapse (`←` on a closed row goes to its parent) |
| `Esc` | close the session panel, cancel a prompt |
| `t` | toggle live transcript tracking |
| `m` | message the session (on a group: all its live sessions) |
| `g` / `u` | add the session to a group / remove it from one |
| `N` | [start a new session](/guide/launching) in a terminal window |
| `a` | bring a session ACC started back on screen (re-attach tmux, raise its window) |
| `r` | rename the session in ACC (empty reverts to the agent's title) |
| `n` | new group (pre-filled as a child of the selected group) |
| `c` / `C` | cycle colour / type a `#rrggbb` colour |
| `d` | delete the selected group and its subgroups |
| `e` | hide or show ended sessions |
| `$` | show or hide cost (token counts stay visible) |
| `q` | quit |

F-keys also work (`F1` help, `F2` message, `F3`/`F4` group, `F5` new group,
`F6` colour, `F8` delete, `F10` quit). The mouse works too: click to focus,
click a selected row to open it, scroll with the wheel. Toggles such as
`$` and `e` are remembered in `~/.acc/tui.json`.

## Web

```sh
acc open
```

![The web dashboard in dark mode, with a working Claude Code session and its goal open](/screens/web-dark.png)

The web dashboard has the same layout: the tree on the left, the opened
session on the right, and a footer with connection state, plan limits,
the "need you" count and total cost. It also adds:

- a **Jump to session** box above the tree (`/` focuses it): it lists the
  sessions matching what you type, `↑↓` choose and `Enter` opens one,
- a colour picker for every group and session,
- a message composer under the session (`Ctrl/⌘ + Enter` sends),
- **✉** on a group row (or right-click it) to message all of its live sessions,
- an **Auto / Dark / Light** theme switch and a **$ shown / hidden** toggle,
  both remembered by your browser,
- **+ Session** to [start a session](/guide/launching), **✎** to rename one
  in ACC, and **↗ bring back** for sessions ACC started,
- deep links, for example `/#s=<session-id>&t=1` opens a session with its
  transcript.

![An OpenCode session in the light theme: its goal is met, and usage comes from OpenCode](/screens/web-opencode-light.png)

![A session waiting for permission to run rm -rf build](/screens/web-permission.png)

The screenshots use made-up sessions: `scripts/demo.sh` starts a throwaway
daemon filled with them, which is also a quick way to try the dashboards.
