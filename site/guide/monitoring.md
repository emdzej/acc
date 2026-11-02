# Monitoring

## Session states

<span class="legend"><span class="working">working</span><span class="waiting">waiting</span><span class="idle">idle</span><span class="ended">ended</span></span>

The coloured dot shows a session's state, derived from its latest hook event:
*active* (started), *working* (prompt or tools running), *waiting*
(notification or permission prompt), *idle* (turn finished), *ended*.

## When an agent needs you

When an agent is waiting on you, its status dot is replaced by a badge:

| Badge | Meaning | Set by | Cleared by |
|---|---|---|---|
| ⚠ | needs permission | `PermissionRequest`, permission notifications (also from subagents) | the tool running, or a new prompt |
| ? | asked a question | `AskUserQuestion`, `ExitPlanMode` (plan approval), elicitation dialogs | the tool completing |
| ! | needs attention | any other agent notification | the next prompt or tool |
| ⏎ | awaiting input | turn finished, idle-prompt notification | your next prompt |

The status bar shows **⚑ N need you** for the first three. The web dashboard
also puts the count in the browser tab title.

## Subagents

The session list shows main sessions only. Subagents, such as Claude Code's
Agent/Task tool, nest under their session. `⑂2` means two are running. Expand
a session with `→` (or ▸ in the web UI) to see them, and open one to see its
own activity and final result. A subagent's tool calls never overwrite its
parent's "last tool".

## Session panel

When you open a session, the panel shows:

- id, working directory, groups, colour, model, context-window fill;
- usage: cost, tokens (input, output, cache read and write), lines changed;
- **Latest**: the last tool call with its input and output, or the last
  message, prompt or notification;
- messages you sent, each marked ✓ delivered or ⧗ queued.

### Live transcript

Press `t` (or click **Transcript (live)**) to follow the session's transcript
as it is written: prompts, replies, thinking, tool calls and results. It is
off by default. ACC reads only what was added since the last read, starting
from the last 256 KB, and only `.jsonl` files under `~/.claude/projects`.
Scroll up to read back, and scroll to the bottom to resume following.

## Titles

A session is titled, in order of preference, with the title you set in ACC
(`r` in the TUI, ✎ in the web UI, or `acc rename`), the name you gave it with
`/rename`, Claude Code's auto-generated title, or the working directory's
name. An ACC title stays in ACC: Claude Code's `/resume` list keeps the
agent's own name. ACC checks active transcripts for `/rename` and title
changes every few seconds, because renaming fires no hook.

## Usage and cost

Cost, tokens and lines changed come from the `cost-state` records Claude Code
writes into each transcript. ACC recomputes them at turn boundaries and at most
every 15 seconds while a session is busy. `$` (TUI) or the **$** footer button
(web) hides money everywhere while keeping token counts.

## Plan limits

The 5-hour and weekly limits, with their reset times, appear in the status
bar, for example `5h 80% ↻15:00 · 7d 42%`. They come from Claude Code's
status line feed, which `acc setup` installs. Claude Code also shows them in
its own status line:

```
Opus 5.5 · ctx 37% · 5h 80% ↻15:00 · 7d 42% ↻Fri 02:20
```

These limits belong to your account, not to a session. They update while any
Claude Code session is open; when none is, ACC shows the last values with
their age.
