# Monitoring

## Session states

<span class="legend"><span class="working">working</span><span class="waiting">waiting</span><span class="idle">idle</span><span class="ended">ended</span></span>

The coloured dot shows a session's state, derived from its latest event:
*active* (started), *working* (prompt or tools running), *waiting*
(notification or permission prompt), *idle* (turn finished), *ended*.
Claude Code reports through its hooks, OpenCode through the ACC plugin; both
map to the same states.

A session [started from ACC](/guide/launching) shows *starting* until the
agent reports in, and *ended* if it never does within 10 minutes (for example
when the window was closed at a dialog).

A Claude Code session whose process has exited without ending (a closed
terminal, a crash) is shown as *ended* too: ACC checks Claude Code's registry
of running sessions. See [Resume, history and forget](/guide/history).

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

A subagent started in the background keeps its session *working* after the
main agent's turn ends, so a session isn't shown idle (or awaiting your
input) while work is still running. It settles to idle when the last
subagent stops. A subagent that hasn't reported anything for 30 minutes is
no longer counted, so a lost `SubagentStop` can't keep a session busy forever.

## Session panel

When you open a session, the panel shows:

- id, working directory, groups, colour, model, context-window fill;
- where it runs, for sessions ACC started (tmux or a Ghostty window), and
  whether messages arrive instantly (**⚡ live channel**, or the OpenCode
  plugin connected);
- the session's [goal](#goals), if it has one;
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
Messages ACC delivered over the channel show as **← acc** rather than as raw
channel markup. The live transcript is available for Claude Code sessions;
for OpenCode, the Latest view shows the last tool call, reply or prompt.

## Goals

Claude Code and OpenCode can keep a session working towards an objective:
`/goal all tests in ./internal pass` re-prompts the agent after each turn
until a judge decides the goal is met. ACC shows the goal in the session
panel:

```
goal ◎ active  all tests in ./internal pass   round 2   2 tests in internal/store still fail
```

| Status | Meaning |
|---|---|
| active | the agent is working towards it; the text after it is the judge's latest verdict |
| paused | OpenCode paused it, for example after a failed turn; `/goal resume` continues |
| met | the judge decided the objective is reached |
| failed | the judge decided it can't be reached |

Clearing the goal (`/goal clear`) removes it from the panel. You can set and
clear goals from ACC too, by sending `/goal …` as a [message](/guide/messaging#slash-commands).

Claude Code records goals in the transcript, which ACC checks every few
seconds. OpenCode reports them to the ACC plugin as they change, with the
round count and the round limit.

## Titles

A session is titled, in order of preference, with the title you set in ACC
(`r` in the TUI, ✎ in the web UI, or `acc rename`), the name you gave it with
`/rename`, Claude Code's auto-generated title, or the working directory's
name. An ACC title stays in ACC: Claude Code's `/resume` list keeps the
agent's own name. ACC checks active transcripts for `/rename` and title
changes every few seconds, because renaming fires no hook. OpenCode reports
its session titles, including renames, through the plugin.

## Usage and cost

Cost, tokens and lines changed come from the `cost-state` records Claude Code
writes into each transcript. ACC recomputes them at turn boundaries and at most
every 15 seconds while a session is busy. For OpenCode, cost and tokens
(reasoning counts as output) come from its running session totals. The
footer adds up the cost of every session. `$` (TUI) or the **$** footer button
(web) hides money everywhere while keeping token counts.

## Plan limits

The 5-hour and weekly limits, with their reset times, appear in the status
bar, for example `5h 80% ↻15:00 · 7d 42%`. They come from Claude Code's
status line feed, which `acc setup` installs. Claude Code also shows them in
its own status line:

```
Opus 5.5 · ctx 37% · 5h 80% ↻15:00 · 7d 42% ↻Fri 02:20
```

### Will you hit the limit?

ACC forecasts each window from its recent pace: the usage rate over the last
45 minutes for the 5-hour window, or the last 6 hours for weekly windows.
With less history than that, it uses the average since the window opened.

If at that pace the window runs out before it resets, a **⚠** with the
expected time appears next to it: `5h 80% ⚠15:39 ↻16:53`. In the web UI, hover
the window for the details: when you'd hit it, how long before the reset,
and the projected usage. An idle stretch lowers the pace, so the warning
clears when you slow down.

The forecast needs the window's length, so it covers Claude Code's 5-hour and
weekly windows. OpenCode doesn't report plan limits, so ACC has none to show
for it.

These limits belong to your account, not to a session. They update while any
Claude Code session is open; when none is, ACC shows the last values with
their age.
