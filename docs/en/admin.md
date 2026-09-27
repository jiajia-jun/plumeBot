# Admin Backend Guide

PlumeBot ships an admin web console: manage group config, persona, group profile, jargon and member facts from the browser, read-only views of runtime state and conversation history, plus log search. A full HTTP API is exposed for scripts and further development.

## 1. Overview

- **Listen**: loopback `127.0.0.1` by default — not exposed to the LAN; `admin.enabled: false` turns it off (only the `/ping` liveness probe remains);
- **Port**: `9321` by default, auto-incremented when occupied (scanning up to 10024); the actual port is printed in the startup log;
- **Auth**: JWT (HS256) bearer tokens; the `admin_user` table is the sole source of accounts (config carries no credentials);
- **First admin**: while `admin_user` is empty, the first visitor registers the single admin account; afterwards registration is closed (4031);
- **Frontend**: a single-page HTML (vanilla fetch, zero build chain), embedded in the binary via go:embed.

## 2. Quick start

```bash
# 1. Start the bot (admin.enabled defaults to true)
./bot.exe

# 2. Open a browser (use the actual port from the startup log)
#    http://127.0.0.1:9321/

# 3. First visit: register the first admin account → log in
```

## 3. Frontend pages

| Tab | What it does |
|-----|--------------|
| Group config | list configured groups; per-group edit of trigger mode / state-rule params / group-management switch; delete a config row (back to global fallback) |
| Persona | edit `system_prompt` / display name per agent — **effective from the very next message** |
| Group profile | per-group culture / topics / active hours / rules / atmosphere; delete restores "no profile" |
| Jargon review | list per-group pending / confirmed; confirm / delete / add (defaults to confirmed, injecting into the prompt immediately) |
| Member facts | view / add / delete facts by `group_id + user_id` (fix what the agent got wrong) |
| Runtime state | read-only energy / cooldown / consecutive counts per session (**read-only, no write endpoint**) |
| Conversation history | active-session dropdown; window messages as bubbles plus an "earlier summaries" block (the compressed history), read-only |
| Logs | search by `level × time range × trace_id` with paging (multi-file scan, pricier than config reads: the level toggles only re-render cached results, "load more" appends by offset) |

## 4. HTTP API reference

- **Prefix**: `/api/v1`; everything except `register` / `login` / `status`, `/ping` and `/` requires `Authorization: Bearer <token>`;
- **Response envelope**:

```json
// success
{ "code": 0, "message": "ok", "data": { ... } }
// failure
{ "code": 4001, "message": "中文错误说明", "data": null }
```

- **Business codes**: `0` ok; `4001` bad param; `4011` unauthenticated / bad credentials; `4031` forbidden; `4041` not found; `4091` conflict; `5000` internal;
- **Rate limiting**: the whole `/api/v1` group uses a per-IP token bucket (10/s, burst 30); `register` / `login` stack a tighter 2/s, burst 10;
- **Write responses** return the updated resource (`data` is the newest state); deletes return `{"code":0}`, and a missing target is `4041`.

### Auth

| Method | Path | Notes |
|--------|------|-------|
| POST | `/api/v1/auth/register` | first-admin registration (no auth): only allowed while `admin_user` is empty; on success issues a token; existing admin → 4031; username conflict → 4091 |
| POST | `/api/v1/auth/login` | login; bcrypt check; unified failure message 4011 (does not disclose whether the user exists) |
| GET | `/api/v1/auth/status` | no auth: whether an admin exists (the frontend picks register vs. login accordingly) |
| PUT | `/api/v1/auth/password` | change password: verify the old one, then update the hash |
| GET | `/api/v1/auth/me` | the current logged-in user (for refresh checks) |

### Group config

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/groups/configs` | all configured groups (including auto-created rows) |
| GET | `/api/v1/groups/:group_id/config` | single group config; unconfigured → 200 with zero fields + `configured:false` |
| PUT | `/api/v1/groups/:group_id/config` | full-row upsert (idempotent); 0/empty columns = fall back to global; `configured` becomes true |
| DELETE | `/api/v1/groups/:group_id/config` | delete the row → back to global fallback (**not persistent**, see §5) |

Validation (fail-fast): `mode` is `mention` / `auto` only; `group_mgmt_enabled` is 0/1 only; numeric params `≥0`; `quiet_hours_*` strictly `"HH:MM"` (equal values = disabled empty window).

### Persona

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/personas` | all agent templates |
| GET | `/api/v1/personas/:agent` | single; missing → 4041 |
| PUT | `/api/v1/personas/:agent` | upsert; **takes effect immediately (from the next message)** |

Validation: `name` trimmed (may be empty); `system_prompt` non-empty with a length cap (≤ 20000).

### Group profile

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/groups/:group_id/profile` | single profile; none → zero values + `configured:false` (profiles are not auto-created, so this state persists) |
| PUT | `/api/v1/groups/:group_id/profile` | upsert `{culture, topics[], active_hours, rules[], atmosphere[]}`; invalidates the memory cache afterwards |
| DELETE | `/api/v1/groups/:group_id/profile` | delete (missing → 4041), cache invalidated too |

> Key point: group profiles go through an in-memory cache, so an admin write must invalidate it (`InvalidateGroupProfile`) or prompts keep reading the stale profile — `service/admin` does this automatically, nothing for you to do.

### Jargon (text always in the request body to avoid URL escaping)

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/groups/:group_id/jargons?status=all\|pending\|confirmed` | list per group; default `all` |
| POST | `/api/v1/groups/:group_id/jargons` | add (defaults to `confirmed` — an admin-approved term is injected into the prompt immediately) |
| DELETE | `/api/v1/groups/:group_id/jargons` | delete; undo a wrong learning / confirmation (missing → 4041) |
| POST | `/api/v1/groups/:group_id/jargons/confirm` | review pending → confirmed (injected from the next message) |

The POST / DELETE / confirm request bodies are all `{"jargon": "..."}`.

### Member facts

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/member-facts?group_id=&user_id=` | list facts; empty `group_id` = private-chat case |
| POST | `/api/v1/member-facts` | add / correct a fact (the usual writer is still the agent's `store_fact`) |
| DELETE | `/api/v1/member-facts?group_id=&user_id=&fact=` | delete one entry (the main fix path when the agent misremembered) |

Validation: `user_id`, `fact` non-empty; `fact` length cap (≤ 500).

### Sessions (read-only)

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/sessions` | active-session dropdown `[{key, count, last_ts, last_render}]` |
| GET | `/api/v1/sessions/:session_key/window` | window messages (chronological) + summary hot chain `{items, summaries}` |
| GET | `/api/v1/sessions/:session_key/state` | read-only runtime state (energy/cooldown/consecutive); no such session → 4041 |

- `session_key`: group = the group number, private = `private:<QQ>` (URL-encoded);
- `window` for an unknown session returns an empty array (not 404); summaries are a display form of "the history compressed before the window" (first- and second-level compression are not distinguished).

### Logs

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/v1/logs` | paged search `{items:[{ts,level,message,trace_id,fields}], has_more}` |

Params: `levels` (csv: info,warn,error,debug; all by default; `error` also includes fatal), `trace_id` (exact match), `begin`/`end` (RFC3339), `limit`/`offset` (default 200/0, cap 1000). Invalid params always return 400.

## 5. How changes take effect (please read)

| Config | Takes effect |
|---------|--------------|
| `group_config` / `persona` | **immediately** (looked up on every message, no cache) — the next message follows the new value |
| `group_profile` | write **+ cache invalidation** (done automatically); everything else in the config file (`control.*`, models, middleware) needs a **restart** |
| global `config.yaml` | restart (no hot reload); the console only manages what is queryable in the DB (persona / profile / etc.) |

**`group_config` auto-create**: the first time a group touches the bot, a snapshot row of the then-effective global values is auto-created. Consequences:

- the group is then **decoupled from globals** — changing `control.mode` / `control.state` in `config.yaml` no longer affects it; edit the group's row instead;
- `configured:false` is reachable only before the group ever touches the bot;
- DELETE is **not persistent** — the next message auto-recreates the row from the then-global values; there is currently no "permanently follow globals" operation (delete again manually if needed).

## 6. Security

- **Loopback binding** by default; remote management requires configuring a listen address plus an internal network / reverse proxy with TLS;
- **Accounts**: passwords stored as bcrypt hashes only; unified login-failure message; per-IP rate limiting on login/register; changing the password requires the old one;
- **Secrets**: an empty `jwt_secret` is auto-generated and persisted to `data/admin_jwt_secret` (0600); tokens/passwords/hashes never go into logs;
- **First-registration gate**: registration closes forever after the first account (fail-closed);
- **Fail-fast validation**: invalid input is always 4xx, never persisted as dirty data (deliberately distinct from the runtime "invalid falls back to default" tolerance);
- **Audit**: writes log a unified `admin config changed` audit (operator/resource/target/IP); no "action" endpoints (sending messages, group management) are exposed — those go through the bot-side Sender / GroupManager channels.

## 7. References

- Interface design & contract details (auto-create, cache invalidation, validation matrix): [admin-web-api-plan.md](../admin-web-api-plan.md)
- Logging conventions & trace_id: architecture [§17](../architecture.md)