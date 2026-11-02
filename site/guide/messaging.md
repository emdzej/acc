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

ACC queues the message, then delivers it one of two ways.

### Instantly, over a channel

`acc setup` registers `acc mcp` as a Claude Code
[channel](https://code.claude.com/docs/en/channels-reference): an MCP server
that Claude Code starts with each session. When you send, the daemon notifies
the channel, which claims the message and pushes it into the session as
`<channel source="acc">`. The message is queued as the session's next prompt,
so **an idle session wakes up and acts on it**; a busy one handles it after
the current step.

Claude Code enables channels per launch, with a flag (there's no settings
key), so start sessions with:

```sh
acc claude [claude args…]       # e.g. acc claude --resume 7116efd3
```

or make it the default once:

```sh
# ~/.zshrc
alias claude='claude --dangerously-load-development-channels server:acc'
```

Claude Code asks you to confirm loading the development channel at each
launch (ACC isn't on Anthropic's channel allowlist). The session panel shows
**⚡ live channel** while the channel is connected.

::: info Requirements
Claude Code 2.1.x with channels: Team and Enterprise orgs need the
`channelsEnabled` policy. Not available with Bedrock, Vertex or Foundry, or in
non-interactive `-p` runs. The channel only claims messages when its session
was started with the flag; otherwise Claude Code would drop them silently, so
they stay queued for the hooks.
:::

### At the next hook

Sessions without a channel collect messages the next time one of their hooks
fires that can carry text back to the model:

| Claude Code hook | How the message arrives |
|---|---|
| `UserPromptSubmit`, `PostToolUse` | added to the model's context (`additionalContext`) inside the running turn |
| `Stop` | the stop is blocked with the message as the reason, so the agent continues and acts on it |

So a working agent receives a message after its next tool call, and an agent
that just finished its turn picks it up immediately. A session idle at its
prompt receives it when it next acts: ⧗ *waiting: delivered when the session
next acts* means the message sits in ACC's queue until then.

### Either way

- **At most once.** A message is marked delivered (✓) as soon as the channel
  or a hook claims it.
- **Main agent only.** Subagents never collect messages. Otherwise a message
  meant for the session would land in a subagent's context.
- **Wording.** Messages are marked so the model knows they come from you
  through ACC and should be treated as user instructions.

Every send is recorded in the daemon's log with its target and sender.

::: warning
A message is an instruction to an agent that can run commands. Anyone who can
reach the ACC API can send one. See [Security](/docs/security).
:::
