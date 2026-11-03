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

ACC queues the message, then delivers it in the way the agent supports:
Claude Code over a channel or at its next hook; Codex, Copilot CLI and
Gemini CLI at their next hook;
OpenCode and pi through ACC's plugin or extension.

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

### OpenCode, through the plugin

The ACC plugin runs inside OpenCode and follows ACC's live stream. When you
send, it claims the message and hands it to OpenCode as a prompt, so **an
idle OpenCode session wakes up and acts on it**, with no flag or alias.
After OpenCode restarts, the plugin picks up the sessions ACC still tracks,
so messages sent in the meantime arrive too. The session panel shows whether
the plugin is connected.

### pi, through the extension

The ACC extension runs inside pi and follows ACC's live stream, like the
OpenCode plugin. It sends each message as a user message, so **an idle pi
session starts a turn on it**. A busy session gets it as a steering message:
after the running tool calls, before the model's next request.

### At the next hook

Claude Code sessions without a channel, and Codex, Copilot CLI and Gemini CLI
sessions, collect messages
the next time one of their hooks fires that can carry text back to the model:

| Hook (Gemini CLI's names in brackets) | How the message arrives |
|---|---|
| `UserPromptSubmit`, `PostToolUse` (`BeforeAgent`, `AfterTool`) | added to the model's context (`additionalContext`) inside the running turn |
| `Stop` (`AfterAgent`) | the stop is blocked with the message as the reason, so the agent continues and acts on it |

So a working agent receives a message after its next tool call, and an agent
that just finished its turn picks it up immediately. A session idle at its
prompt receives it when it next acts: ⧗ *waiting: delivered when the session
next acts* means the message sits in ACC's queue until then.

### Slash commands

A message that is a single line starting with `/`, like `/rename api-fix`,
`/compact`, `/model sonnet` or `/goal all tests pass`, runs as a command
rather than reaching the model as text.

- **OpenCode**: the plugin runs it with OpenCode's command API, for any
  session, including your own custom commands. If OpenCode doesn't know the
  command, the plugin logs the error and the message stays marked delivered.
- **pi**: the extension runs it the way pi runs a typed `/command` from an
  extension, so extension commands, skills (`/skill:name`) and prompt
  templates work. pi's built-in commands such as `/compact` don't.
- **Claude Code, Codex, Copilot CLI and Gemini CLI**: channel and hook
  messages are prompts, which the agents never run as commands, so ACC types the command into the session's
  terminal and presses Enter, the way you would. That needs a terminal ACC
  can type into: sessions [ACC started](/guide/launching), in tmux or a
  Ghostty window. For other sessions the send fails with an explanation; for
  a group, those sessions are skipped and reported. ACC won't type while the
  session shows a permission prompt or a question, where the keystrokes
  would answer the dialog instead.

### Either way

- **At most once.** A message is marked delivered (✓) as soon as the channel
  or a hook claims it.
- **Main agent only.** Subagents never collect messages. Otherwise a message
  meant for the session would land in a subagent's context.
- **Groups.** A group message goes to every live session in the group and
  its subgroups, whatever the agent.
- **Wording.** Messages are marked so the model knows they come from you
  through ACC and should be treated as user instructions.

Every send is recorded in the daemon's log with its target and sender.

::: warning
A message is an instruction to an agent that can run commands. Anyone who can
reach the ACC API can send one. See [Security](/docs/security).
:::
