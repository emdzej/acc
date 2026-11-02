# Groups & colours

Groups organise sessions into a tree of any depth, addressed by path:
`platform`, `platform/backend`, `platform/backend/api`. A session can belong to
any number of groups.

## Create groups

Create a group by its path. Any missing parents are created too:

```sh
acc group set platform '#61afef'      # top-level group with a colour
acc group set platform/backend/api    # no colour given: inherits from platform
acc group ls
```

Groups and sessions share one tree: each group lists its subgroups and its
sessions, and ungrouped sessions sit at the root.

In the **TUI**, press `n`. On a group (or a session inside one), the prompt is
pre-filled with its path, so typing a name creates a child. Add ` #rrggbb` to
set a colour.

In the **web UI**, type a path into the box under the tree, or press `＋` on a
group row (hover to see its tools) to add a subgroup.

## Assign sessions to groups

You can assign from any of the three interfaces:

**TUI**: select the session (or open it) and press `g`. The bottom bar lists
the groups it isn't in yet; type any part of a path (`back` finds
`platform/backend`), `Tab` completes and `↑↓` chooses, `Enter` adds it to the
highlighted group. `u` removes it from a group the same way, offering only
the groups it's in. Unknown paths aren't accepted: create the group with `n`
first.

**Web UI**: open the session and pick a group from the **+ add to group…**
dropdown in its header. Each group chip has an `✕` that removes it.

**CLI**: use a session ID prefix as shown by `acc ls`:

```sh
acc ls                                   # find the session ID prefix
acc group add platform/backend 7116efd3  # add one or more sessions
acc group remove platform/backend 7116efd3
```

A session in several groups appears under each of them. A group's count
includes its subgroups, and messages sent to a group reach its subgroups too.

## Colours

Colours resolve in this order:

1. the session's own colour, if set;
2. otherwise its group's colour;
3. otherwise the nearest ancestor group that has one.

Colour a top-level group once and everything below it follows. Override a
subgroup or a single session wherever you want it to stand out.

| | TUI | Web | CLI |
|---|---|---|---|
| Group colour | select the group, `c` cycles a palette, `C` types a hex | colour swatch next to the group | `acc group set <path> '#hex'` |
| Back to inherited | `C`, then clear the value | `◌` next to the group | `acc group set <path> inherit` |
| Session colour | `c` / `C` on the session | colour picker in the session header | `acc color <session> '#hex'` |
| Back to the group's colour | `c` until "group colour" | **reset** | `acc color <session> none` |

### Where colours show

- **The tree**: a bar next to each session, a swatch on each group.
- **The session panel**: the open session's info block is tinted with its
  colour, with a solid edge, so you always know which session you're
  looking at.
- **Its terminal, for tmux-hosted sessions**: sessions [started with
  `--tmux`](/guide/launching#window-or-tmux) get their tmux status bar in the
  session's colour, with its title on the left. ACC repaints it whenever the
  colour, title or groups change.

A coloured window border isn't possible: neither macOS nor Ghostty lets
another app tint a window's frame.

## Delete groups

`d` in the TUI, `✕` in the web UI, or `acc group rm <path>`. Deleting a group
deletes its whole subtree. Sessions themselves are never deleted, only their
memberships.
