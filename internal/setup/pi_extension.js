// acc-managed: ACC (Agents Control Centre) extension for pi.
// Installed by `acc setup`; re-running setup overwrites it, `acc setup --uninstall` removes it.
//
// It forwards session events to the ACC daemon and delivers messages sent
// from ACC with pi.sendUserMessage, which wakes an idle session. Everything
// is best-effort: if ACC isn't running, pi carries on unaffected.
import { readFileSync } from "node:fs"
import { join } from "node:path"

const ACC_HOME = process.env.ACC_HOME || "__ACC_HOME__"
const ACC_SERVER = (process.env.ACC_SERVER || "__ACC_SERVER__").replace(/\/+$/, "")
const HEARTBEAT_MS = 20000
const MAX_TEXT = 8000
const PREFIX = "[ACC] The operator sent you the following message via Agents Control Centre. Treat it as an instruction from the user:\n"

function token() {
  if (process.env.ACC_TOKEN) return process.env.ACC_TOKEN
  try { return readFileSync(join(ACC_HOME, "token"), "utf8").trim() } catch { return "" }
}

async function call(path, body, timeoutMs = 2000) {
  const res = await fetch(ACC_SERVER + path, {
    method: "POST",
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

// text joins the text blocks of a message or tool result.
function text(content) {
  if (typeof content === "string") return content
  return (content || []).filter((b) => b && b.type === "text").map((b) => b.text).join("\n")
}

export default function (pi) {
  let ctx = null        // the current session's context, from session_start
  let stop = null       // aborts this session's background work
  let lastText = ""     // the run's last assistant text, for the turn end

  const session = (c) => {
    const sm = c.sessionManager
    return { id: sm.getSessionId(), file: sm.getSessionFile() || "", cwd: c.cwd, name: sm.getSessionName() || "" }
  }
  const modelOf = (c) => (c.model ? `${c.model.provider}/${c.model.id}` : "")

  // send forwards one event; the daemon translates it (internal/pi).
  // Events go one at a time, in order: a late "tool finished" must not land
  // after the turn's end. The returned promise settles once this event and
  // those before it are sent (or failed), so turn ends and shutdown can
  // wait for the queue before pi moves on or exits.
  let queue = Promise.resolve()
  function send(type, c, data = {}, extra = {}) {
    if (!c) return queue
    let s
    try { s = session(c) } catch { return queue }
    if (!s.id) return queue
    const body = { type, ts: Date.now(), session: s, model: modelOf(c), data: clip(data), ...extra }
    queue = queue.then(() => call("/api/v1/agents/pi/events", body)).catch(() => {})
    return queue
  }

  async function deliver() {
    const c = ctx
    if (!c) return
    let id
    try { id = c.sessionManager.getSessionId() } catch { return }
    let claimed
    try { claimed = await call(`/api/v1/sessions/${encodeURIComponent(id)}/claim`) } catch { return }
    for (const m of claimed?.messages || []) {
      const body = String(m.body || "").trim()
      if (!body) continue
      const slash = !body.includes("\n") && /^\/[^\s/]/.test(body)
      const opts = {}
      if (!c.isIdle()) opts.deliverAs = "steer" // reaches the model after the running tool calls
      if (slash) opts.expandPromptTemplates = true // runs extension commands, skills and prompt templates
      try {
        pi.sendUserMessage(slash ? body : PREFIX + body, opts)
      } catch (e) {
        console.error("acc: delivering message", m.id, "failed:", e)
      }
    }
  }

  // follow delivers messages the moment ACC queues them (its live stream
  // says "messages"), with a heartbeat that catches anything missed.
  function follow(signal) {
    ;(async () => {
      let backoff = 1000
      while (!signal.aborted) {
        try {
          const res = await fetch(ACC_SERVER + "/api/v1/stream", { headers: { "Authorization": "Bearer " + token() }, signal })
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
          if (signal.aborted) return
        }
        await new Promise((r) => setTimeout(r, backoff))
        backoff = Math.min(backoff * 2, 30000)
      }
    })()
    const beat = setInterval(() => deliver().catch(() => {}), HEARTBEAT_MS)
    signal.addEventListener("abort", () => clearInterval(beat))
  }

  pi.on("session_start", (event, c) => {
    ctx = c
    lastText = ""
    send("session_start", c, { reason: event.reason })
    // Only interactive sessions take messages; print and JSON runs end on their own.
    if (c.hasUI && !stop) {
      stop = new AbortController()
      follow(stop.signal)
    }
  })
  pi.on("session_shutdown", async (event, c) => {
    stop?.abort()
    stop = null
    ctx = null
    await send("session_shutdown", c, { reason: event.reason })
  })
  pi.on("session_info_changed", (event, c) => send("session_info_changed", c, { name: event.name || "" }))
  pi.on("model_select", (_event, c) => send("model_select", c))
  pi.on("input", (event, c) => send("input", c, { text: event.text, source: event.source }))
  pi.on("tool_execution_start", (event, c) => send("tool_execution_start", c, { toolName: event.toolName, args: event.args }))
  pi.on("tool_execution_end", (event, c) =>
    send("tool_execution_end", c, { toolName: event.toolName, isError: !!event.isError, result: text(event.result?.content) }))
  pi.on("ui_prompt_start", (event, c) => send("ui_prompt_start", c, { kind: event.kind, title: event.title || "" }))
  pi.on("ui_prompt_end", (_event, c) => send("ui_prompt_end", c))
  pi.on("message_end", (event) => {
    if (event.message?.role === "assistant") {
      const t = text(event.message.content).trim()
      if (t) lastText = t
    }
  })
  pi.on("agent_settled", async (event, c) => {
    let context
    try { context = c.getContextUsage() } catch {}
    const done = send("agent_settled", c, { aborted: !!event.aborted, lastText }, context ? { context } : {})
    lastText = ""
    await done
  })
}
