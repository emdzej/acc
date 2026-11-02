// acc-managed: ACC (Agents Control Centre) plugin for OpenCode 2.x.
// Installed by `acc setup`; re-running setup overwrites it, `acc setup --uninstall` removes it.
//
// It forwards session events to the ACC daemon and delivers messages sent from
// ACC: prompts with ctx.session.prompt (which wakes an idle session) and /commands
// with ctx.session.command. Everything is best-effort: if ACC isn't running,
// OpenCode carries on unaffected.
import { readFileSync } from "node:fs"
import { join } from "node:path"

const ACC_HOME = process.env.ACC_HOME || "__ACC_HOME__"
const ACC_SERVER = (process.env.ACC_SERVER || "__ACC_SERVER__").replace(/\/+$/, "")
const HEARTBEAT_MS = 20000
const MAX_TEXT = 8000

const GOAL_CHANGED = "rpc.opencode.goal.changed"

// Event types ACC understands; everything else stays inside OpenCode.
const FORWARD = new Set([
  "session.created", "session.deleted", "session.archived", "session.renamed",
  "session.inbox.enqueued", "session.execution.started", "session.execution.succeeded",
  "session.execution.failed", "session.execution.interrupted", "session.step.started",
  "session.tool.called", "session.tool.success", "session.tool.failed", "session.text.ended",
  "session.usage.updated", "permission.asked", "permission.replied",
  "form.created", "form.replied", "form.cancelled",
])

function token() {
  if (process.env.ACC_TOKEN) return process.env.ACC_TOKEN
  try { return readFileSync(join(ACC_HOME, "token"), "utf8").trim() } catch { return "" }
}

async function call(path, body, timeoutMs = 2000, method = "POST") {
  const res = await fetch(ACC_SERVER + path, {
    method,
    headers: { "Authorization": "Bearer " + token(), "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(timeoutMs),
  })
  if (!res.ok) throw new Error(path + ": " + res.status)
  return res.status === 204 ? null : res.json()
}

// clip keeps forwarded payloads small: tool output can be huge.
function clip(v) {
  if (typeof v === "string") return v.length > MAX_TEXT ? v.slice(0, MAX_TEXT) + "…" : v
  if (Array.isArray(v)) return v.slice(0, 50).map(clip)
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, clip(x)]))
  return v
}

const PREFIX = "[ACC] The operator sent you the following message via Agents Control Centre. Treat it as an instruction from the user:\n"

export default {
  id: "acc",
  setup(ctx) {
    const stop = new AbortController()
    const sessions = new Set() // sessions this OpenCode instance has shown activity for
    const tools = new Map()    // tool call id -> name (OpenCode names a tool only when its input starts)
    const lastText = new Map() // session -> last assistant text of the turn
    // OpenCode's /goal state, read through its RPC (no schema: ACC validates).
    let goals = null
    try { goals = ctx.rpc({ id: "opencode.goal", methods: { get: {} }, events: { changed: {} } }) } catch {}
    const sendGoal = async (sessionID) => {
      if (!goals) return
      const goal = await goals.get({ sessionID })
      await call("/api/v1/agents/opencode/events", { type: GOAL_CHANGED, created: Date.now(), data: { sessionID, goal: goal ?? null } })
    }

    async function deliver() {
      for (const sessionID of sessions) {
        let claimed
        try { claimed = await call(`/api/v1/sessions/${encodeURIComponent(sessionID)}/claim`) } catch { return }
        for (const m of claimed?.messages || []) {
          const body = String(m.body || "").trim()
          const cmd = !body.includes("\n") && /^\/([^\s/]\S*)\s*(.*)$/.exec(body)
          try {
            if (cmd) await ctx.session.command({ sessionID, name: cmd[1], text: cmd[2] })
            else await ctx.session.prompt({ sessionID, text: PREFIX + body })
          } catch (e) {
            console.error("acc: delivering message", m.id, "to", sessionID, "failed:", e)
          }
        }
      }
    }

    // After a restart or reload, pick up the sessions ACC still tracks for
    // OpenCode, so idle ones get their messages without acting first. Only
    // sessions this instance can open are added: claiming a message for a
    // session elsewhere would lose it.
    ;(async () => {
      const all = await call("/api/v1/sessions", undefined, 5000, "GET")
      for (const s of all || []) {
        if (s.agent !== "opencode" || s.status === "ended" || sessions.has(s.id)) continue
        try {
          if (await ctx.session.get({ sessionID: s.id })) {
            sessions.add(s.id)
            sendGoal(s.id).catch(() => {})
          }
        } catch {}
      }
      if (sessions.size) await deliver()
    })().catch(() => {})

    // Forward goal changes (/goal set, judged, paused, cleared).
    const offGoals = goals?.events.on("changed", (ev) => {
      const sid = ev.data?.sessionID
      if (!sid) return
      sessions.add(sid)
      call("/api/v1/agents/opencode/events", { type: GOAL_CHANGED, created: ev.created, location: ev.location, data: ev.data }).catch(() => {})
    })

    // Forward events.
    ;(async () => {
      try {
        for await (const ev of ctx.event.subscribe({ signal: stop.signal })) {
          const d = ev.data || {}
          const sid = d.sessionID || d.form?.sessionID
          if (ev.type === "session.tool.input.started") tools.set(d.id, d.name)
          if (ev.type === "session.text.ended" && sid) lastText.set(sid, d.text)
          if (!FORWARD.has(ev.type) || !sid) continue
          const known = sessions.has(sid)
          sessions.add(sid)
          const out = { type: ev.type, created: ev.created, location: ev.location, data: clip(d) }
          if (ev.type.startsWith("session.tool.")) {
            out.acc_tool = tools.get(d.id) || ""
            if (ev.type !== "session.tool.called") tools.delete(d.id)
          }
          if (ev.type.startsWith("session.execution.") && ev.type !== "session.execution.started") {
            out.acc_last_text = clip(lastText.get(sid) || "")
            lastText.delete(sid)
          }
          if (ev.type === "session.deleted") sessions.delete(sid)
          call("/api/v1/agents/opencode/events", out).catch(() => {})
          if (!known) deliver().catch(() => {}) // a newly seen session may have messages waiting
        }
      } catch (e) {
        if (!stop.signal.aborted) console.error("acc: event subscription ended:", e)
      }
    })()

    // Deliver messages the moment ACC queues them (its live stream says
    // "messages"), with a heartbeat that catches anything missed.
    ;(async () => {
      let backoff = 1000
      while (!stop.signal.aborted) {
        try {
          const res = await fetch(ACC_SERVER + "/api/v1/stream", {
            headers: { "Authorization": "Bearer " + token() }, signal: stop.signal,
          })
          if (!res.ok || !res.body) throw new Error("stream: " + res.status)
          backoff = 1000
          const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
          let buf = ""
          for (;;) {
            const { value, done } = await reader.read()
            if (done) break
            buf += value
            let i
            while ((i = buf.indexOf("\n")) >= 0) {
              const line = buf.slice(0, i).trim()
              buf = buf.slice(i + 1)
              if (line === "event: messages" || line === "event: hello") deliver().catch(() => {})
            }
          }
        } catch {
          if (stop.signal.aborted) return
        }
        await new Promise((r) => setTimeout(r, backoff))
        backoff = Math.min(backoff * 2, 30000)
      }
    })()
    const beat = setInterval(() => deliver().catch(() => {}), HEARTBEAT_MS)

    return () => {
      stop.abort()
      clearInterval(beat)
      offGoals?.()
    }
  },
}
