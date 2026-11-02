# Messaging sessions

ACC can send an instruction into a running agent session, for example "stop
and rebase on main" or "also update the changelog". Pick a single session or a
whole group.

```sh
acc send 7116efd3 "please run the tests before committing"
acc send --group platform "pause after your current task"
```

In the TUI press `m`. In the web UI use the composer under the opened session,
or right-click a group to message all of its live sessions.

## How delivery works

Messages are queued in ACC, and the session collects them the next time one of
its hooks fires that can carry text back to the model:

| Claude Code hook | How the message arrives |
|---|---|
| `UserPromptSubmit`, `PostToolUse` | added to the model's context (`additionalContext`) inside the running turn |
| `Stop` | the stop is blocked with the message as the reason, so the agent continues and acts on it |

So a working agent receives a message after its next tool call, and an agent
that just finished its turn picks it up immediately. Some details:

- **Nothing is paused.** ⧗ *waiting: delivered when the session next acts*
  means the message sits in ACC's queue. ACC can't wake a session; a session
  idle at its prompt receives it when it next acts: you type a prompt, it runs
  a tool, ends a turn, or is started or resumed. The mark then turns into ✓.
  (Pushing into an idle session needs Claude Code channels; see the
  [roadmap](/docs/roadmap).)
- **At most once.** A message is marked delivered as soon as a hook collects
  it.
- **Main agent only.** Subagents' hooks never collect messages. Otherwise a
  message meant for the session would land in a subagent's context.
- **Wording.** Messages are prefixed so the model knows they come from you
  through ACC and should be treated as user instructions.

Every send is recorded in the daemon's log with its target and sender.

::: warning
A message is an instruction to an agent that can run commands. Anyone who can
reach the ACC API can send one. See [Security](/docs/security).
:::
