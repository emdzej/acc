# Dashboards

The TUI and the web dashboard show the same data and offer the same actions.
Use whichever suits you; they update live and can run side by side.

## Terminal (TUI)

```sh
acc tui
```

<!-- generated: ACC_DOCS_SCREEN=/tmp/s.ansi go test ./internal/tui -run DocsScreen && scripts/ansi2html.py /tmp/s.ansi -->
<pre class="tui"><span style="color:#585858">╭─ </span><span style="color:#808080">Groups</span><span style="color:#585858"> ───────────────────</span><span style="color:#585858">╮</span><span style="color:#585858">╭─ </span><span style="color:#808080">api-server · claude-code</span><span style="color:#585858"> ──────────────────────</span> <span style="color:#e5c07b">● working</span> <span style="color:#585858">╮</span>
<span style="color:#585858">│</span><span style="color:#808080;background:#3a3a3a"> ◇ </span><span style="background:#3a3a3a;font-weight:700">All</span><span style="background:#3a3a3a">                    </span><span style="color:#808080;background:#3a3a3a">3 </span><span style="color:#585858">│</span><span style="color:#585858">│</span> <span style="color:#808080">id </span>7116efd3-730d<span style="color:#808080">   cwd </span>/Users/me/code/api                  <span style="color:#585858">│</span>
<span style="color:#585858">│</span><span style="color:#808080"> ▾ </span><span style="color:#61afef">● </span>platform             <span style="color:#808080">1 </span><span style="color:#585858">│</span><span style="color:#585858">│</span> <span style="color:#808080">groups </span><span style="color:#61afef">● </span>platform/api  <span style="color:#808080">  colour </span><span style="color:#808080">inherits</span>                   <span style="color:#585858">│</span>
<span style="color:#585858">│</span><span style="color:#808080">     </span><span style="color:#61afef">◦ </span>api                <span style="color:#808080">1 </span><span style="color:#585858">│</span><span style="color:#585858">│</span> <span style="color:#808080">started </span>13:17:14<span style="color:#808080">   seen </span>2s<span style="color:#808080">   subagents </span>1<span style="color:#e5c07b"> (1 running)</span><span style="color:#c678dd">   ✉ 1…</span><span style="color:#585858">│</span>
<span style="color:#585858">│</span><span style="color:#808080">   </span><span style="color:#c678dd">● </span>web                  <span style="color:#808080">1 </span><span style="color:#585858">│</span><span style="color:#585858">│</span> <span style="color:#808080">model </span>Opus 5.5<span style="color:#808080">   context </span><span style="color:#e06c75">82%</span><span style="color:#808080"> of 200.0k</span> <span style="color:#e06c75">━━━━━━━━━━━━━━━━</span><span style="color:#808080">────</span><span style="color:#585858">│</span>
<span style="color:#585858">│</span><span style="color:#808080"> ○ </span><span style="color:#808080">ungrouped</span>              <span style="color:#808080">1 </span><span style="color:#585858">│</span><span style="color:#585858">│</span> <span style="color:#808080">usage </span><span style="font-weight:700">$0.42</span>  in 1.2k · out 5.3k · cache 2.4M read / 0 writ…<span style="color:#585858">│</span>
<span style="color:#585858">╰────────────────────────────╯</span><span style="color:#585858">│</span> <span style="color:#e06c75;font-weight:700">⚠ needs permission</span><span style="color:#808080"> for 5s</span>  Bash: rm -rf build              <span style="color:#585858">│</span>
<span style="color:#61afef">╭─ </span><span style="color:#61afef;font-weight:700">Sessions</span><span style="color:#61afef"> ──────────────</span> <span style="color:#808080">3</span> <span style="color:#61afef">╮</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span><span style="font-weight:700"> </span><span style="color:#808080">┃</span><span style="color:#808080">  </span><span style="color:#585858">● </span><span style="color:#808080">scratch</span>            <span style="color:#808080">1m </span><span style="color:#61afef">│</span><span style="color:#585858">│</span><span style="font-weight:700"> Latest</span><span style="color:#808080">   t: track transcript live</span>                          <span style="color:#585858">│</span>
<span style="color:#61afef">│</span><span style="font-weight:700"> </span><span style="color:#e06c75">┃</span><span style="color:#808080">  </span><span style="color:#c678dd">● </span>web-ui             <span style="color:#808080">9s </span><span style="color:#61afef">│</span><span style="color:#585858">│</span> <span style="color:#e5c07b">⚙ Bash</span><span style="color:#808080">  PostToolUse · 13:17:17</span>                             <span style="color:#585858">│</span>
<span style="color:#61afef">│</span><span style="background:#264f78;font-weight:700">›</span><span style="color:#61afef;background:#264f78">┃</span><span style="color:#808080;background:#264f78">▾ </span><span style="color:#e06c75;background:#264f78;font-weight:700">⚠ </span><span style="background:#264f78;font-weight:700">api-server</span><span style="background:#264f78">   </span><span style="color:#e5c07b;background:#264f78">⑂1 </span><span style="color:#c678dd;background:#264f78">✉1 </span><span style="color:#808080;background:#264f78">2s </span><span style="color:#61afef">│</span><span style="color:#585858">│</span>   go test ./...                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span><span style="color:#808080">    └ </span><span style="color:#e5c07b">● </span>Explore <span style="color:#808080">Grep</span>     <span style="color:#808080">0s </span><span style="color:#61afef">│</span><span style="color:#585858">│</span> <span style="color:#808080">output</span>                                                     <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>   <span style="color:#808080">ok      github.com/acme/api/internal/store    0.31s</span>      <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>   <span style="color:#808080">ok      github.com/acme/api/internal/http    0.12s</span>       <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span>                                                            <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span><span style="font-weight:700"> Messages</span>                                                   <span style="color:#585858">│</span>
<span style="color:#61afef">│</span>                            <span style="color:#61afef">│</span><span style="color:#585858">│</span> <span style="color:#98c379">✓ </span><span style="color:#808080">13:17:17 </span>please run the tests before committing          <span style="color:#585858">│</span>
<span style="color:#61afef">╰────────────────────────────╯</span><span style="color:#585858">╰────────────────────────────────────────────────────────────╯</span>
<span style="color:#000000;background:#61afef;font-weight:700"> ACC </span><span style="color:#98c379;background:#303030"> ● live </span><span style="color:#808080;background:#303030">127.0.0.1:7317 </span><span style="color:#e5c07b;background:#303030">◆ embedded (stops on quit) </span><span style="color:#e06c75;background:#303030;font-weight:700">⚑ 1 need you </span><span style="color:#e06c75;background:#303030">5h 80%</span><span style="color:#808080;background:#303030"> ↻15:17</span><span style="background:#303030">  </span><span style="color:#98c379;background:#303030">7d 42%</span><span style="background:#303030">  </span><span style="color:#808080;background:#303030">…</span>
 <span style="color:#61afef;font-weight:700">?</span> <span style="color:#808080">help</span>   <span style="color:#61afef;font-weight:700">⏎</span> <span style="color:#808080">open</span>   <span style="color:#61afef;font-weight:700">m</span> <span style="color:#808080">msg</span>   <span style="color:#61afef;font-weight:700">g</span> <span style="color:#808080">group</span>   <span style="color:#61afef;font-weight:700">u</span> <span style="color:#808080">ungroup</span>   <span style="color:#61afef;font-weight:700">c</span> <span style="color:#808080">colour</span>   <span style="color:#61afef;font-weight:700">→</span> <span style="color:#808080">subagents</span>   <span style="color:#61afef;font-weight:700">e</span> <span style="color:#808080">ended</span>   <span style="color:#61afef;font-weight:700">$</span> <span style="color:#808080">cost</span> …</pre>

The screen has three panels:

- **Groups** (top left) is the group tree. Selecting a group filters the
  session list to it and its subgroups.
- **Sessions** (bottom left) lists main sessions only. `→` expands a session
  to show its subagents.
- **Session** (right) shows the session you opened with `Enter`, or a subagent.

The bottom bar shows the shortcuts for the focused panel; `?` lists them all.

| Key | Action |
|---|---|
| `Tab` / `Shift-Tab` | switch panel |
| `↑↓` `j k` `PgUp/PgDn` | move or scroll |
| `Enter` or click | open the session or subagent |
| `→ ←` `Space` | expand or collapse subagents or child groups |
| `Esc` | close the session panel, cancel a prompt |
| `t` | toggle live transcript tracking |
| `m` | message the session (in Groups: the whole group) |
| `g` / `u` | add the session to a group / remove it from one |
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

The web dashboard has the same layout: groups and sessions on the left, the
opened session on the right, and a footer with connection state, plan limits,
the "need you" count and total cost. It also adds:

- a colour picker for every group and session,
- a message composer under the session (`Ctrl/⌘ + Enter` sends),
- right-click a group to message all of its live sessions,
- an **Auto / Dark / Light** theme switch and a **$ shown / hidden** toggle,
  both remembered by your browser,
- deep links, for example `/#s=<session-id>&t=1` opens a session with its
  transcript.
