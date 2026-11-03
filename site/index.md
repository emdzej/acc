---
layout: home

hero:
  name: ACC
  text: Agents Control Centre
  tagline: Watch, organise and interact with your Claude Code, Codex, Copilot CLI, Gemini CLI, OpenCode and pi sessions, from a terminal or a browser. One Go binary.
  image:
    src: /logo.png
    alt: ACC
  actions:
    - theme: brand
      text: Get started
      link: /guide/
    - theme: alt
      text: How it works
      link: /docs/how-it-works
    - theme: alt
      text: GitHub
      link: https://github.com/emdzej/acc

features:
  - title: Every session at a glance
    details: Live status for every Claude Code, Codex, Copilot CLI, Gemini CLI, OpenCode and pi session (working, waiting, idle, ended) in one tree, with subagents nested under their session and the latest tool call one keypress away.
    link: /guide/monitoring
  - title: Knows when an agent needs you
    details: Permission prompts, questions and plan approvals show as a badge on the session, and the status bar counts how many sessions are waiting on you.
    link: /guide/monitoring#when-an-agent-needs-you
  - title: Messages that wake idle sessions
    details: Send an instruction to one session or a whole group. It arrives at once over a Claude Code channel or the OpenCode plugin, even at an idle prompt. Slash commands like /compact work too.
    link: /guide/messaging
  - title: Start sessions from ACC
    details: Open a new session in Ghostty, Terminal, iTerm or a tmux session you can detach from and bring back, already titled, grouped and coloured.
    link: /guide/launching
  - title: Group trees and colours
    details: Organise sessions in nested groups like platform/backend/api. Colours inherit down the tree, tint the session panel and paint the tmux status bar.
    link: /guide/groups
  - title: Usage, limits and a pace forecast
    details: Cost, tokens and context fill per session, your plan's 5-hour and weekly limits, and a warning when the current pace would hit a limit before it resets.
    link: /guide/monitoring#plan-limits
  - title: Goals in view
    details: When a session works towards a /goal, ACC shows the objective, its status, the round and the judge's latest verdict.
    link: /guide/monitoring#goals
  - title: One binary, three faces
    details: A full-screen TUI, a web dashboard and a CLI in one static Go binary. acc setup wires your agents in and acc tui starts everything.
    link: /guide/
---

<div class="home-shots">

![The ACC web dashboard with made-up sessions from every agent](./screens/web-dark.png)

![The ACC terminal dashboard in Ghostty](./screens/tui.png)

![History: every session the agents have on record](./screens/web-history.png)

</div>
