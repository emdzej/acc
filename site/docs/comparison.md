# How ACC compares

ACC is a **session manager** for coding agents. It shows every session your
agents run, which ones need you, and what they cost. It lets you organise
those sessions and talk to them. It doesn't orchestrate agents, split work
between them or replace the terminal you run them in. Several tools cover
nearby ground, and they make different choices.

*Based on each project's own documentation, November 2026. Corrections are
welcome: [open an issue](https://github.com/emdzej/acc/issues).*

## The main difference: observing, not hosting

Most multi-agent tools **host** your agents. You start each session inside
them, in their terminal panes, their tmux sessions or their desktop app, and
they only know about the sessions they started. ACC **observes** them
instead. The agents report to ACC through their own hooks or plugins, so
ACC sees every session wherever it runs: a Ghostty tab, an IDE terminal, a
tmux pane, or a session ACC started for you.

Two things follow from that:

- **Nothing changes in how you work.** Keep your terminal, your multiplexer
  and your agent's own UI. Install the integration once, with `acc setup`,
  and every session shows up.
- **Status comes from the agent itself, not from the screen.** "Needs
  permission" appears because the agent's hook said so, not because a
  pattern matched the bottom of a terminal pane. Tools that detect status
  from the screen can miss a prompt they don't recognise.

## At a glance

| | Interface | Sessions it didn't start | Isolation | Organising | Usage and limits | Orchestration |
|---|---|---|---|---|---|---|
| **ACC** | TUI, web, CLI | **yes** (hooks, plugins) | none | tree of coloured groups | tokens, cost, context, plan limits with forecast | no |
| [herdr](https://github.com/herdrdev/herdr) | its own terminal multiplexer | no: agents run in its panes | none | workspaces, tabs, panes | not documented | primitives for agents to drive each other |
| [Agent Deck](https://github.com/asheshgoplani/agent-deck) | TUI on tmux, web | no | worktrees, Docker | nested groups | cost and budgets | yes (a conductor session) |
| [AgentPulse](https://github.com/jstuart0/agentpulse) | web | **yes** (hooks) | none | not documented | not documented | optional (launch templates, supervisors) |
| [Claude Squad](https://github.com/smtg-ai/claude-squad) | TUI on tmux | no | git worktree per task | none | none | no |
| [CCManager](https://github.com/kbwo/ccmanager) | TUI | no | worktrees, devcontainers | by project | none | no |
| [Vibe Kanban](https://github.com/BloopAI/vibe-kanban) | web | no | workspace per issue | kanban board | not documented | task-board workflow |
| [opcode](https://github.com/winfunc/opcode) | desktop | reads Claude Code's history | none | projects | cost dashboard | custom agents |
| [Sculptor](https://github.com/imbue-ai/sculptor) | desktop | no | worktrees | workspaces | not documented | light |

"Not documented" means the project's documentation doesn't say.

## Where ACC is the better fit

- **You already run agents in different terminals**, and want one place that
  shows them all without moving them anywhere.
- **You use several agents.** Claude Code, Codex, Copilot CLI, Gemini CLI,
  OpenCode and pi show up in one tree, with one set of states and one
  [History](/guide/history) of everything they have on disk.
- **You care about limits.** ACC shows Claude Code's and Codex's 5-hour and
  weekly windows and [warns when your pace](/guide/monitoring#will-you-hit-the-limit)
  will hit one before it resets, along with each session's context use and cost.
- **You want to reach sessions, not just watch them.** You can message one
  session or a whole group, run slash commands, and start or resume sessions
  already titled, grouped and coloured.

## Where other tools are the better fit

- **One isolated workspace per task.** Agent Deck, CCManager, Claude Squad
  and Sculptor create a git worktree (or a container) per session. ACC
  leaves that to you and your agent.
- **Review, merge and pull requests.** Vibe Kanban and Sculptor are built
  around reviewing an agent's diff and shipping it. ACC doesn't touch your
  code.
- **Many more agents, or many machines.** herdr recognises about 30 agents
  and puts several SSH machines in one window. Its server keeps agents
  running when you detach. AgentPulse relays sessions across machines.
- **Agents coordinating agents.** herdr's socket API and Agent Deck's
  conductor let agents spawn and drive each other. ACC doesn't orchestrate.
  That may come later, but it isn't the goal.

These tools aren't mutually exclusive. ACC can watch agents that run inside
a host such as herdr or Claude Squad, as long as the agents have ACC's hooks
installed.
