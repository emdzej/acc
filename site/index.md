---
layout: home

hero:
  name: ACC
  text: Agents Control Centre
  tagline: Watch, group, colour and message all your coding-agent sessions, from a terminal or a browser. One Go binary.
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
  - icon: 🛰️
    title: Every session at a glance
    details: Live status for every Claude Code session (working, waiting, idle, ended), with subagents nested under their parent and the latest tool call one keypress away.
  - icon: ⚠️
    title: Knows when an agent needs you
    details: Permission prompts, questions and plan approvals light up as a badge on the session, and the status bar counts how many sessions are waiting on you.
  - icon: ✉️
    title: Message sessions back
    details: Send instructions to one session or a whole group. ACC injects them through the agent's own hooks at its next turn, with no copy and paste.
  - icon: 🗂️
    title: Group trees & colours
    details: Organise sessions in nested groups like platform/backend/api. Colours inherit down the tree, and any session can override its own.
  - icon: 📈
    title: Usage & plan limits
    details: Per-session cost, tokens and context fill, plus your plan's 5-hour and weekly limits with reset times, fed by Claude Code's status line.
  - icon: 🧰
    title: One binary, three faces
    details: A full-screen TUI, a web dashboard and a CLI in one static Go binary. `acc setup` wires your agents in and `acc tui` starts everything.
---
