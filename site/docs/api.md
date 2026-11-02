# HTTP API

The daemon's JSON API, used by the dashboards, the CLI and the hooks. Every
request needs `Authorization: Bearer <token>`, or the `acc_token` cookie set
by `acc open`. Errors look like `{"error": "…"}`.

## Sessions

| Method & path | Description |
|---|---|
| `GET /api/v1/sessions` | All sessions, newest activity first, with groups, subagents (up to 20 per session), usage, context, attention, `host`, `channel` and `goal` (`objective`, `status`, `reason`, `rounds`, `max_rounds`). |
| `POST /api/v1/sessions` | `{"cwd", "title", "group", "color", "prompt", "tmux"}` opens a terminal running a new session; returns `{"session_id", "terminal", "command", "host"}`. The command comes from `~/.acc/config.json`. |
| `POST /api/v1/sessions/{id}/type` | `{"text", "force"}` types text and Enter into a session ACC started (`host` is set). Refuses while a permission or question dialog is open unless `force`. |
| `POST /api/v1/sessions/{id}/attach` | Brings a session ACC started back on screen. |
| `PATCH /api/v1/sessions/{id}` | `{"color": "#rrggbb"}` sets the session's colour, `{"title": "…"}` its ACC title; `""` clears either. |
| `GET /api/v1/sessions/{id}/latest?agent=` | Latest tool call and output, message, prompt or notification, for the main agent or a subagent. |
| `GET /api/v1/sessions/{id}/transcript?agent=&offset=` | Transcript entries after a byte `offset` (`-1` = the last 256 KB). Pass the returned `offset` back to follow. |
| `GET /api/v1/sessions/{id}/events?limit=` | Recent raw lifecycle events. |
| `GET /api/v1/sessions/{id}/messages?limit=` | Messages sent to the session, with delivery times. |

`{id}` accepts any unique session ID prefix wherever a session is written to.

## Messages

`POST /api/v1/messages`

```json
{ "session_id": "7116efd3", "body": "run the tests" }
{ "group": "platform/backend", "body": "pause after this task" }
```

Returns `{"queued": [Message…]}`. A group send reaches every non-ended session
in the group and its subgroups.

## Groups

| Method & path | Description |
|---|---|
| `GET /api/v1/groups` | The tree, depth-first, with `path`, `depth`, own `color` and inherited `display_color`. |
| `POST /api/v1/groups` | `{"name": "a/b", "color": "#rrggbb"}` creates or updates a group; `color: ""` inherits. |
| `DELETE /api/v1/groups/{path}` | Delete a group and its subtree. |
| `POST /api/v1/assign` | `{"session_id", "group", "remove": false}` adds a membership or removes one. |

## Plan usage

`GET /api/v1/plan` returns per-agent rate-limit windows:

```json
[{ "agent": "claude-code", "updated_at": 1793620837466,
   "windows": [{ "key": "five_hour", "label": "5h", "used_pct": 80, "resets_at": 1793628037000,
                  "projected_pct": 120, "limit_at": 1793626237000, "pace": "recent" }] }]
```

`projected_pct` is where the window ends up at the current pace, and
`limit_at` (only when the forecast reaches 100%) is when it runs out. `pace`
says whether the forecast used the recent rate (`recent`) or the average
since the window opened (`window`).

## Live updates

`GET /api/v1/stream` is a Server-Sent Events stream. It sends `hello` on
connect, `sessions` whenever data changed (refetch what you show),
`messages` when a message is queued, and a
keep-alive comment every 25 seconds.

## Agent endpoints

| Method & path | Description |
|---|---|
| `POST /api/v1/events` | A normalised hook event. With `deliver: true`, the response is `{"messages": […]}` claimed for injection. |
| `POST /api/v1/sessions/{id}/claim` | Channel servers (`acc mcp`): claim the session's queued messages for immediate delivery. Also marks the channel live (`channel: true` on the session) for 60 seconds. |
| `POST /api/v1/status` | A status line report: session model and context, plus plan windows. |
| `POST /api/v1/agents/opencode/events` | A raw OpenCode bus event from the ACC plugin; ACC maps it to lifecycle events, title, usage, model and goal. |
