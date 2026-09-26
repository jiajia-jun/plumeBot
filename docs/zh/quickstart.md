# 快速开始

PlumeBot 是下在 OneBot v11 协议上的 QQ「赛博群友」，对接 [NapCat](https://github.com/NapNeko/NapCatQQ)。除 NapCat 外无任何外部服务依赖：单二进制、纯 Go 无 cgo、数据落在本地 SQLite。

## 环境要求

| 依赖 | 版本 | 用途 |
|------|------|------|
| Go | 1.26+（见 `go.mod`） | 编译与运行 |
| Docker + Docker Compose | — | 启动 NapCat（QQ 登录端） |
| QQ 账号 | — | NapCat 登录，供 bot 挂机 |
| LLM 端点 | 任意 OpenAI 兼容 | 对话 / 记忆 / 摘要推理 |

> 目前测试验证的 LLM 端点为 OpenAI 兼容格式（如 DeepSeek、Ollama），`base_url` 填入兼容地址即可。

## 第 1 步：启动 NapCat

NapCat 负责 QQ 登录与 OneBot 消息收发，用 docker-compose 启动：

```bash
# 在项目根目录创建 .env（已被 .gitignore 忽略，不会入库），填入 QQ 号
echo "PLUMEBOT_SELFID=<你的QQ号>" >> .env
echo "WEBUI_TOKEN=<随便一个管理台密码>" >> .env

docker compose up -d
docker compose logs -f napcat   # 首启看日志里的二维码，用手机 QQ 扫码
```

登录成功后：

- OneBot WS 地址为 `ws://127.0.0.1:3001`，与 `config.yaml` 的默认 `onebot.ws_url` 一致，无需改动；
- 登录态持久化在 Docker 命名卷 `napcat-qq`，重启免重复扫码；
- NapCat WebUI 在 `http://127.0.0.1:6099`（如不需要可不开端口）。

`PLUMEBOT_SELFID` 同时供 `config.yaml` 的 `bot.self_id`（环境变量覆盖）与 docker-compose 的 `ACCOUNT` 使用，只管填一次即可。

## 第 2 步：配置模型

首次运行会自动生成默认 `config.yaml`（嵌入二进制的模板），只需填写模型 API Key：

```yaml
llm:
  models:
    - name: chat
      provider: openai            # OpenAI 兼容接口
      base_url: https://api.deepseek.com/v1
      api_key: "sk-xxxx"          # 你的 API Key
      model: deepseek-chat
  chat_model: chat
```

也可以不把密钥写进配置文件，改用环境变量 `PLUMEBOT_APIKEY_<模型名大写>` 注入（模型名 `chat` → `PLUMEBOT_APIKEY_CHAT`），密钥不落盘。

> 其它可选配置：图片条视模型（`llm.vision_model`）、触发模式与状态规则（`control`）、敏感词表（`middleware.sensitive_words`）等，见 [配置参考](configuration.md)。

## 第 3 步：编译运行

```bash
go build -o bot.exe ./cmd/bot/
./bot.exe
```

启动日志会打印各初始化步骤。把 bot 拉进群，**@ 它即可开始对话**；也可直接用 QQ 私聊它。

> 提供了一键交叉编译脚本 `build.bat`（Windows / Linux 双平台，`go build -trimpath -ldflags "-s -w"`）。

## 第 4 步：管理控制台（可选）

bot 内置管理 Web 控制台，浏览器访问：

```
http://127.0.0.1:9321/
```

- 端口默认 `9321`，被占用时自动逐次 +1（扫描至 10024），实际监听端口打印在启动日志；
- 首次访问会进入**注册页**——`admin_user` 表为空时第一个到达者创建唯一管理员账号，之后注册入口关闭；
- 控制台可管理群配置 / 人格 / 群画像 / 黑话 / 成员事实，只读查看运行态与对话历史，并检索日志。

详见 [管理后端指南](admin.md)。

## 验证安装

运行后查看日志：

```bash
ls ~/.plumebot/logs/
```

`info.log` 中应看到每条消息的**入口行**与**结局行**：

```text
{"level":"info","message":"收到消息","message_id":"...","group_id":"...","user_id":"...","message_type":"group","mentioned":true}
{"level":"info","message":"消息结局","outcome":"agent_replied",...}
```

`outcome: agent_replied` 表示触发且回复发送成功。日志规范见 [功能指南 · 日志与运维](features.md)。

## 故障排查

| 现象 | 检查 |
|------|------|
| 启动报 `receive message: read tcp ... websocket` | NapCat 未启动或 `onebot.ws_url` 不对；确认 `docker compose ps`、NapCat 为正向 WS（`MODE=ws`） |
| LLM 调用 401 / 报错 | `llm.models[0].api_key`（或对应 `PLUMEBOT_APIKEY_*`）与 `model` 名是否正确；`base_url` 是否兼容 |
| 改 `config.yaml` 不生效 | 全局配置（control 默认值、中间件、模型条目）**需要重启**生效；只有 `persona` / `group_config` / `group_profile` 等**库内配置**改库即生效（见各文档） |
| 管理页打不开 | 确认 `admin.enabled: true`；端口被占用会自动 +1，见启动日志实际端口 |
| static 下拉 403 / API 4011 | token 过期或未登录，回登录页重新登录 |

## 下一步

- [配置参考](configuration.md) —— config.yaml 全字段说明
- [功能指南](features.md) —— 记忆、触发控制、人格、群管理等能力设计
- [插件开发指南](plugin-dev.md) —— 编写第三方插件
- [管理后端指南](admin.md) —— Web 控制台与 HTTP API