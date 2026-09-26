# Configuration Reference

## The config file

- **Location**: `config.yaml` in the working directory (local file, gitignored — may contain real keys, never committed);
- **Generation**: on first run, if the file does not exist it is written from the embedded default template (`pkg/config/config.default.yaml`) and loaded;
- **Template sync**: after a main-binary update, `config.yaml` and `config.default.yaml` may gain new fields — keeping the two in sync is a manual duty;
- **Fallbacks**: empty / absent fields are not written to config; the consumer keeps a code-level default (values in the tables below are those fallbacks).

## Environment overrides

Only **sensitive / machine-specific** fields read from the environment (no general env reading):

| Variable | Overrides | Notes |
|----------|-----------|-------|
| `PLUMEBOT_APIKEY_<MODEL_NAME_UPPERCASE>` | the matching model entry's `api_key` | e.g. `name: chat` → `PLUMEBOT_APIKEY_CHAT`; takes precedence over the file value when non-empty; entries with an empty `name` are skipped |
| `PLUMEBOT_SELFID` | `bot.self_id` | shared with the docker-compose `ACCOUNT` |

```bash
export PLUMEBOT_APIKEY_CHAT=sk-xxx
export PLUMEBOT_SELFID=10001
./bot.exe
```

## Section overview

| Section | Purpose | Takes effect |
|---------|---------|--------------|
| `bot` | bot identity | restart |
| `onebot` | NapCat connection | restart |
| `log` | log level | restart |
| `control` | trigger mode + state-rule global defaults | restart (a `group_config` row decouples a group from globals; see [features](features.md)) |
| `middleware` | rate limiting + sensitive words | restart |
| `llm` | model entries / chat & vision refs / prompt budget | restart |
| `tools` | enabled tools | restart |
| `agent` | agent metadata + global fallback persona | restart |
| `admin` | admin backend service | restart |

## `bot`

| Field | Default | Description |
|-------|---------|-------------|
| `name` | `PlumeBot` | display name (QQ side); empty → `PlumeBot` |
| `self_id` | empty | the bot's own QQ number; overridden by `PLUMEBOT_SELFID` |

## `onebot`

| Field | Default | Description |
|-------|---------|-------------|
| `ws_url` | `ws://127.0.0.1:3001` | NapCat "WebSocket server" address (forward WS); empty → default |
| `access_token` | `""` | fill in if NapCat is configured with an access token |

## `log`

| Field | Default | Description |
|-------|---------|-------------|
| `level` | `info` | gate: `debug` / `info` / `warn` / `error`; `fatal.log` is always on |

Logs are split into per-level files under `~/.plumebot/logs/` (`debug/info/warn/error/fatal.log` + gin access `gin.log`); see [features · logging](features.md).

## `control`

Global defaults for the trigger mode and state rules. `mode` defaults to `mention`; each `state` parameter defaults to a code-level value when 0 / empty (shown below).

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | `mention` | `mention` (reply only when @'d or in private chat) \| `auto` (the agent decides whether to join ordinary group messages) |
| `state.energy_max` | 100 | energy cap |
| `state.energy_cost` | 10 | energy spent per reply |
| `state.energy_recover` | 5 | recovery points per minute |
| `state.energy_threshold` | 20 | below this, no proactive speech (@ still answered) |
| `state.cooldown_seconds` | 60 | min interval between proactive replies (seconds) |
| `state.consecutive_limit` | 5 | consecutive-reply cap; hit it → forced rest |
| `state.rest_seconds` | 300 | forced rest duration (seconds) |
| `state.quiet_hours_start` | `23:00` | quiet-period start `"HH:MM"` |
| `state.quiet_hours_end` | `07:00` | quiet-period end `"HH:MM"` (may cross midnight) |
| `state.short_message_chars` | 4 | ignore messages shorter than N chars |

> These are only per-group *defaults*. The first time a group touches the bot it gets a snapshot row auto-created in `group_config`; afterwards the group is **decoupled from globals** (changing `control.*` in `config.yaml` no longer affects it — edit the row in the admin console instead).

## `middleware`

| Field | Default | Description |
|-------|---------|-------------|
| `rate_limit.rate` | 2 | token refill rate (tokens/sec); ≤0 → 2 |
| `rate_limit.burst` | 20 | bucket capacity (allowed burst); ≤0 → 20 |
| `rate_limit.max_wait_seconds` | 10 | wait timeout; on timeout reply a fixed message and drop; ≤0 → 10 |
| `sensitive_words` | `[]` | word list; on hit reply "我拒绝回答" and drop; empty = no filtering (Aho-Corasick matcher) |

## `llm`

| Field | Default | Description |
|-------|---------|-------------|
| `timeout_seconds` | 60 | per-call LLM timeout (seconds); ≤0 → 60 |
| `native_multimodal` | `false` | native multimodal mode (placeholder for a future stage; off) |
| `models` | — | model entries (below) |
| `models[].name` | — | your label; referenced by `chat_model` / `vision_model` |
| `models[].provider` | `openai` | provider: `openai` (OpenAI-compatible); empty → `openai` |
| `models[].base_url` | `https://api.openai.com/v1` | OpenAI-compatible endpoint |
| `models[].api_key` | `""` | API key; local models (Ollama) may leave empty; overridable via `PLUMEBOT_APIKEY_<NAME_UPPER>` |
| `models[].model` | — | model name; **empty → startup error** (not guessable) |
| `models[].temperature` | not sent | sampling temperature; absent → model default |
| `models[].max_tokens` | not sent | max output tokens; ≤0 → not sent |
| `chat_model` | `models[0].name` | chat model entry ref; empty → first entry |
| `vision_model` | `""` | image-description model entry ref; empty = image description off |
| `prompt.facts_per_member` | 3 | per-member fact injection cap in the persona block; ≤0 → 3 |
| `prompt.jargon_cap` | 20 | confirmed jargon injection cap; ≤0 → 20 |
| `prompt.describe_recent_rounds` | 5 | only images in the last N rounds are described; ≤0 → 5 |
| `prompt.describe_per_turn_cap` | 10 | description quota per message; ≤0 → 10 |

Enabling a vision model for image description requires adding an entry **and** setting `vision_model`:

```yaml
llm:
  models:
    - name: chat
      provider: openai
      base_url: https://api.deepseek.com/v1
      api_key: "..."
      model: deepseek-chat
    - name: vision
      provider: openai
      base_url: "..."
      api_key: "..."
      model: qwen-vl-plus
  chat_model: chat
  vision_model: vision
```

## `tools`

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | — | enabled tool names; empty = no tools registered |

Built-in tools:

- Memory: `store_fact` / `learn_jargon` / `forget_fact` (the agent writes `member_facts` / `group_jargon` via tool calling);
- Group management: `group_mute` / `group_unmute` / `group_kick` / `group_set_card` (guarded by the per-group switch and admin check — see [features](features.md)).

## `agent`

| Field | Default | Description |
|-------|---------|-------------|
| `name` | `PlumeBot` | agent id (adk metadata, for multi-agent routing); empty → `PlumeBot` |
| `description` | default | capability description (adk metadata) |
| `system_prompt` | default persona | global fallback persona; used only when no `persona` row exists for this agent. Maintain personas in the admin console "persona" page instead — changes take effect immediately (see [features](features.md)) |

## `admin`

Service configuration for the admin backend (holds **no** account / password — the sole source of accounts is the `admin_user` table).

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `true` | admin API + frontend switch; `false` → only the `/ping` liveness probe remains |
| `port` | `9321` | gin listen start port; auto-increments on conflict (scan up to 10024), warns if the whole range is taken |
| `jwt_secret` | empty | JWT signing key; if empty, generated on first start and persisted to `data/admin_jwt_secret` (0600), so tokens survive restarts |
| `token_ttl_seconds` | `86400` | token TTL (seconds); ≤0 → 86400 |

See the [admin backend guide](admin.md).