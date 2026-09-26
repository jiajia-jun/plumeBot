# Quick Start

PlumeBot is an AI-driven QQ "cyber community member" built on the OneBot v11 protocol, connecting to [NapCat](https://github.com/NapNeko/NapCatQQ). It has no service dependencies other than NapCat: a single static binary, pure Go without cgo, all data in a local SQLite database.

## Prerequisites

| Dependency | Version | Purpose |
|------------|---------|---------|
| Go | 1.26+ (see `go.mod`) | Build and run |
| Docker + Docker Compose | — | Run NapCat (the QQ login side) |
| A QQ account | — | Used by NapCat to log in and hold the bot online |
| An LLM endpoint | any OpenAI-compatible | Conversation / memory / summarization |

> Currently tested against OpenAI-compatible endpoints (e.g. DeepSeek, Ollama); put the compatible address in `base_url`.

## Step 1: Start NapCat

NapCat handles QQ login and OneBot messaging. Start it with docker-compose:

```bash
# Create a .env file in the repo root (gitignored; never committed) with your QQ number
echo "PLUMEBOT_SELFID=<your QQ number>" >> .env
echo "WEBUI_TOKEN=<pick a password for the NapCat web UI>" >> .env

docker compose up -d
docker compose logs -f napcat   # first start: scan the QR code shown in the log with your phone QQ
```

After login succeeds:

- The OneBot WebSocket server is at `ws://127.0.0.1:3001`, which matches the default `onebot.ws_url` in `config.yaml` — no change needed;
- The login session persists in the Docker named volume `napcat-qq`, so restarts do not require re-scanning;
- The NapCat web UI is at `http://127.0.0.1:6099` (skip the port if you don't need it).

`PLUMEBOT_SELFID` is shared by both the `bot.self_id` config (via environment override) and the docker-compose `ACCOUNT` — set it once.

## Step 2: Configure your model

The first run generates a default `config.yaml` from the embedded template. Fill in the model API key:

```yaml
llm:
  models:
    - name: chat
      provider: openai            # OpenAI-compatible endpoint
      base_url: https://api.deepseek.com/v1
      api_key: "sk-xxxx"          # your API key
      model: deepseek-chat
  chat_model: chat
```

You can also keep the key out of the file and inject it via the environment variable `PLUMEBOT_APIKEY_<MODEL_NAME_UPPERCASE>` (`name: chat` → `PLUMEBOT_APIKEY_CHAT`); the key never lands on disk.

> Optional configuration such as a vision model (`llm.vision_model`), trigger/state rules (`control`), and the sensitive-word list (`middleware.sensitive_words`) is covered in the [configuration reference](configuration.md).

## Step 3: Build and run

```bash
go build -o bot.exe ./cmd/bot/
./bot.exe
```

The startup log prints each initialization step. Invite the bot to a group and **@ it to start chatting** — it also answers private messages.

> A helper script `build.bat` cross-compiles for Windows and Linux (`go build -trimpath -ldflags "-s -w"`).

## Step 4: Admin console (optional)

The bot ships with an admin web console:

```
http://127.0.0.1:9321/
```

- Default port `9321`; if occupied, it auto-increments scanning up to `10024`. The actual port is printed in the startup log;
- On first visit you reach a **registration page** — while the `admin_user` table is empty, the first visitor creates the sole admin account; afterwards registration is closed;
- The console manages group config / persona / group profile / jargon / member facts, read-only views of runtime state and conversation history, and log search.

See the [admin backend guide](admin.md).

## Verify the install

After running, check the logs:

```bash
ls ~/.plumebot/logs/
```

`info.log` should contain an **entry line** and an **outcome line** for each message:

```text
{"level":"info","message":"收到消息","message_id":"...","group_id":"...","user_id":"...","message_type":"group","mentioned":true}
{"level":"info","message":"消息结局","outcome":"agent_replied",...}
```

`outcome: agent_replied` means the message triggered the bot and the reply was sent. Logging conventions are described in the [features guide](features.md).

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Startup logs `receive message: read tcp ... websocket` | NapCat is not running or `onebot.ws_url` is wrong; verify `docker compose ps` and that NapCat is a forward WS server (`MODE=ws`) |
| LLM 401 / errors | `llm.models[0].api_key` (or the matching `PLUMEBOT_APIKEY_*`) and `model` name; is `base_url` an OpenAI-compatible endpoint? |
| `config.yaml` changes have no effect | Global settings (control defaults, middleware, model entries) require a **restart**. Only in-database config (persona / group_config / group_profile) takes effect immediately (see the guides) |
| Admin page won't open | Check `admin.enabled: true`; if the port is occupied the server auto-increments — watch the startup log for the actual port |
| API 4011 | Token expired or not logged in; go back to the login page |

## Next steps

- [Configuration reference](configuration.md) — every `config.yaml` field
- [Features guide](features.md) — memory, trigger control, persona, group management
- [Plugin development](plugin-dev.md) — write third-party plugins
- [Admin backend guide](admin.md) — web console and HTTP API