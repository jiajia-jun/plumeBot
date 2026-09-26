[简体中文](README.zh-CN.md) |
[Documentation](docs/README.md) |
[Contributing](CONTRIBUTING.md) |
[Architecture](docs/architecture.md) |
[Roadmap](docs/roadmap.md)

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white) ![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux-blue)

# PlumeBot

*PlumeBot — an AI-driven "cyber community member" for QQ.*

PlumeBot is not a command robot. Built on the [OneBot v11](https://github.com/botuniverse/onebot-11) protocol and [NapCat](https://github.com/NapNeko/NapCatQQ), it lives in your group as a member with **memory, personality and restraint**: it eavesdrops on the conversation, remembers facts about people and groups, decides *when* to speak, and guards its own privileges.

Written in Go. Ships as a single static binary with no cgo — no service dependencies other than NapCat.

## Features

- **Three-tier memory** — an in-memory context window (20 → 100 rounds); old context compressed by the LLM into summaries (level-1 compress, level-2 merge, FIFO eviction, archived to SQLite and reloaded on restart); plus long-term *member facts* and *group jargon* that the agent reads and writes itself via tool calling.
- **Personality as data** — persona templates live in SQLite and take effect from the very next message. No restart, no config-file juggling.
- **Human-like restraint** — `mention` / `auto` trigger modes plus a pure-rule state machine (energy, cooldown, consecutive-reply cap, quiet hours, short-message skip) so it never floods the group.
- **Multi-modal perception** — images are lazily described by an optional vision model, cached and budgeted per turn.
- **Guarded group management** — the agent can mute / unmute / kick / set-card, but every action funnels through one guarded entry: per-group switch + real-time admin check (fail-closed) + a 30-day mute clamp.
- **Isolated plugins** — third-party plugins run as separate child processes (go-plugin) behind a zero-permission intent protocol; they depend only on the [`plumebot-sdk`](https://github.com/plumebot/plumebot-sdk) module, never on host internals.
- **Ops-friendly** — DDD layering, versioned DB migrations, structured logs with a per-session `trace_id`, and a built-in admin console (config, conversation history, log search).

## Quick start

Requires Docker (for NapCat), a QQ account and an OpenAI-compatible LLM endpoint.

```bash
# 1. Start NapCat (QQ login); scan the QR code shown in the log
cp .env.example .env                                  # edit: PLUMEBOT_SELFID, WEBUI_TOKEN
docker compose up -d && docker compose logs -f napcat

# 2. First run generates config.yaml — fill in your model API key
go build -o bot.exe ./cmd/bot/ && ./bot.exe
```

Then just **@ the bot**. Full walkthrough and troubleshooting: [Quick Start](docs/en/quickstart.md).

## Documentation

- [Documentation home](docs/README.md) — quick start, full configuration reference, features guide, plugin development, admin backend
- [简体中文](README.zh-CN.md)
- [Architecture design](docs/architecture.md) · [Roadmap](docs/roadmap.md) · [Admin API plan](docs/admin-web-api-plan.md)

## License

MIT — see [LICENSE](LICENSE).