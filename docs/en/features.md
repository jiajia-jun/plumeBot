# Features Guide

A user- and developer-facing tour of what PlumeBot does and where the boundaries are. For implementation details, the [architecture document](../architecture.md) is the single source of truth.

## 1. Message pipeline

Every incoming message runs through a fixed pipeline before any reply is decided:

```
NapCat → OneBot WS
  ├── [1] log          —— every message is logged first (including ones that are later rejected)
  ├── [2] rate limit   —— token bucket: degrades a burst (on timeout replies a fixed message and drops)
  ├── [3] sensitive words —— Aho-Corasick matcher; on hit replies "我拒绝回答" and drops
  ├── [4] persist      —— written to the in-memory context window + SQLite messages table
  │
  ├── command message (starts with /) → plugin dispatch → reply
  └── ordinary message → trigger check (§3) → hit → full reply loop
```

Even messages rejected by rules (rate limited, blocked, not triggered) have already been **eavesdropped** into the window and storage — so the context is always complete when the bot is @'d.

## 2. Memory system

### 2.1 Three tiers

| Tier | Carrier | Content | Lifetime |
|------|---------|---------|----------|
| Short-term | in-memory ring buffer | the last 20 → 100 rounds | process life (full messages are also in SQLite) |
| Mid-term | LLM-compressed summaries | old messages compressed when the window fills; summaries merged again, FIFO-evicted; hot chain in memory, archive in SQLite, reloaded on restart | keeps rolling with the window |
| Long-term | SQLite `member_facts` / `group_jargon` | member facts & group jargon (**the agent reads and writes them itself via tools**) | persistent |

- **Member facts**: atomic `(group_id, user_id, fact)` entries, each with an independent lifecycle (dedupe / delete); empty `group_id` = private-chat memory, non-empty = group memory (the same person may have different facts in different groups);
- **Group jargon**: `group_jargon` has a **pending → confirmed** review state machine; only confirmed entries are injected into the prompt; review in the admin console (see [admin](admin.md));
- **Session key convention**: group = `GroupID`, private = `private:`+UserID — used across the window, summary archive, runtime state, and trigger-control state.

### 2.2 Five-part prompt assembly

Each triggered reply is assembled live in five parts:

```
① system persona    —— system_prompt from the persona table template (who you are)
② session profile   —— group profile (culture/topics/jargon) + in-window member facts (N per member)
③ compressed summary—— historical context (concatenated)
④ current window    —— the last N rounds of raw messages
⑤ current message   —— the message being handled
```

Profile (aggregate labels) is bounded and rewritten as a whole; facts/jargon (atomic entries) grow unbounded and are read at assembly, written via tools. Injection volume is capped by `llm.prompt.*` — tokens never grow without bound.

## 3. Trigger control

### 3.1 Trigger modes

| Mode | Behavior |
|------|----------|
| `mention` | replies only when @'d or in private chat; still eavesdrops every message |
| `auto` | always replies to @ and private chat; the agent freely decides whether to join ordinary group messages |

The mode controls only **whether to reply**, not whether messages are received.

Each group is configured independently (the `group_config` table). The first time a group touches the bot, a snapshot row is auto-created from the then-effective global values; after that the group is **decoupled from globals** (see [admin](admin.md) §5).

### 3.2 State rules (pure rule layer, no LLM)

In auto mode, before the agent decides on an ordinary message, five rules gate it:

| Rule | Default | Description |
|------|---------|-------------|
| Energy | cap 100, cost 10/reply, recover 5/min, no proactive speech below 20 | a sense of "battery"; don't be noisy |
| Cooldown | 60 s | min interval between two proactive replies |
| Consecutive cap | 5, then forced 300 s rest | no runaway spamming |
| Quiet hours | silent 23:00–07:00 (may cross midnight) | stays out of deep-night chatter; @ still answered |
| Short-message skip | <4 chars | tiny messages don't trigger the agent |

**Rule boundary**: they constrain **proactive speech only** (ordinary messages in auto mode). Being @'d / private chat = forced reply, bypassing all state rules (rules are idle in mention mode). **Rejected ≠ discarded**: a rejected message is simply not replied to; it is still eavesdropped and cached.

Every parameter can be overridden per group in the admin console (a non-zero/non-empty `group_config` column overrides the global default).

## 4. Persona

The persona is not a system-prompt line in a config file; it is a **template** in the SQLite `persona` table (bound to an agent name):

- **Live edits**: `BuildMessages` looks up the template on every assembly, so editing `system_prompt` takes effect on the very next message — no restart;
- **Fallback chain**: `persona` template `system_prompt` → `config.agent.system_prompt` → built-in default persona;
- **Maintenance**: edit in the admin console "persona" page (PUT = immediate), or directly in the database.

## 5. Multi-modal perception

Images are **lazily described** into the conversation:

- only images in the **last N rounds + the current message** are described (`llm.prompt.describe_recent_rounds`) to control budget;
- descriptions are cached in memory (no repeated model calls for the same image) and written back to storage for reuse;
- enable by adding a vision model entry and setting `llm.vision_model` (see [configuration](configuration.md)); when disabled, images degrade to `[图片]` in the prompt.

## 6. AI group management

The agent can autonomously run four tools to manage a group: `group_mute` / `group_unmute` / `group_kick` / `group_set_card`. Every action is funneled into a **single guarded entry** with three guards:

1. **Per-group switch**: `group_config.group_mgmt_enabled` (auto-created rows default to 1 = on; set 0 to disable);
2. **Admin check** (fail-closed): both the caller and the bot must be group owner/admin — roles are queried live; a failed/unresolvable query is always denied;
3. **Duration clamp**: mute duration is capped at 30 days.

Action results are reported one by one (via `APIResponse` checks); failures never go silent. This is a high-privilege capability — guards and thresholds are tight by design. Plugin `Actions` share the **same guard chain** as AI tools.

## 7. Memory tools

The agent decides at conversation time whether to call them (there is no query tool — the data is small, direct injection is more reliable):

- `store_fact`: record a member fact (writes `member_facts`);
- `learn_jargon`: learn a group jargon term (goes to pending, awaiting review);
- `forget_fact`: delete a recorded fact.

Enable/disable via `tools.enabled` ([configuration](configuration.md)).

## 8. Logging & operations

### 8.1 Log files

Logs go to files only (not the terminal), split per exact level:

| File | Records |
|------|---------|
| `debug.log` | Debug only: window / profile / compression / send-success and other high-frequency detail |
| `info.log` | Info only: the message ledger (entry + outcome), model calls, write audit, startup milestones |
| `warn.log` | Warn only: rejected messages, guard denies, per-step failures, 429/401 |
| `error.log` | Error only: service-level failures (with stacktrace) |
| `fatal.log` | Fatal only: fatal startup errors |
| `gin.log` | per-request gin access logs |

Rotation: 10 MB per file / 5 backups / 30 days. Directory: `~/.plumebot/logs/`.

### 8.2 Message ledger

Each message gets **exactly one entry line and one outcome line**: entry → `Info("收到消息", …)`; outcome → `Info|Warn("消息结局", …, outcome)`. Common outcomes: `agent_replied` (triggered and sent) / `not_triggered` / `rate_limited` / `sensitive` / `command` / per-stage failures. Grep `消息结局` to reconcile the final fate of any message.

### 8.3 trace_id

Key-path logs carry a trace_id: admin requests = client IP, QQ side = `group:<GroupID>` / `private:<QQ>`. Entry → outcome → model call of one session can be chained by trace_id; the console log page searches by it.

## 9. Architecture overview

```
cmd ──→ handler ──→ service ──→ domain (interfaces)
                      │
                      └──→ infra (compile-time injection)

infra ──→ domain (implements interfaces)
domain: zero dependencies
```

- **Layering**: domain (pure interfaces + entities, no third-party imports) → service (orchestration, depends on interfaces) → infra (implements, wraps third-party libraries);
- **Single binary**: pure Go without cgo (`modernc.org/sqlite`), runs the same on desktops and servers;
- **Versioned migrations**: `schema_migrations` applies each migration in a single transaction with a version record; a failure rolls back and retries on next start;
- **Locking**: per-session locks — different groups/private chats never block each other, and no lock is ever held across an LLM/IO call.

Full design: [architecture.md](../architecture.md).