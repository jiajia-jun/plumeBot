[English](README.md) |
[文档索引](docs/README.md) |
[贡献指南](CONTRIBUTING.zh-CN.md) |
[架构设计](docs/architecture.md) |
[开发规划](docs/roadmap.md)

![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white) ![License](https://img.shields.io/badge/license-MIT-blue.svg) ![Platform](https://img.shields.io/badge/platform-Windows%20%7C%20Linux-blue)

# PlumeBot

*PlumeBot —— 基于 OneBot v11 协议的 QQ「赛博群友」。*

PlumeBot 不是命令机器人。它基于 [OneBot v11](https://github.com/botuniverse/onebot-11) 协议接入 [NapCat](https://github.com/NapNeko/NapCatQQ)，以一个有记忆、有人格、懂分寸的**群成员**身份存在于你的群里：它旁听群聊、记住人与群的点点滴滴、自主判断**什么时候说话**，并为自己的高危能力设好护栏。

Go 语言实现，单二进制部署，纯 Go 无 cgo——除 NapCat 外无任何外部服务依赖。

## 项目亮点

- **三级记忆系统** —— 内存上下文窗口（20 → 100 轮）；窗口满后由 LLM 压成摘要（一级压缩 → 二级融合 → FIFO 淘汰，热链在内存、归档落 SQLite、重启自动回灌）；另有长期**成员事实**与**群黑话**，Agent 在对话中经 tool calling 自主读写；
- **人格即数据** —— 人格模板存于 SQLite，改库即时生效。无需重启，无需折腾配置文件；
- **懂分寸** —— `mention` / `auto` 双触发模式 + 一条纯规则状态机（精力值、冷却、连续回复上限、深夜静默、短消息忽略），不会刷屏、不会半夜吵人；
- **多模态感知** —— 图片惰性描述接入对话，可选视觉模型启用，带缓存与每轮预算控制；
- **护栏先行的群管理** —— Agent 可禁言 / 解禁 / 踢人 / 改名片，但所有动作收敛到唯一执行入口：群开关 + 实时管理员校验（fail-closed）+ 禁言时长钳制 30 天；
- **隔离的插件体系** —— 第三方插件以独立子进程运行（go-plugin），走零权限意图协议，只依赖 [`plumebot-sdk`](https://github.com/plumebot/plumebot-sdk) 模块，不碰宿主内部；
- **开箱可运维** —— DDD 分层、版本化数据库迁移、带 `trace_id` 的结构化日志，内置管理控制台（群配置 / 对话历史 / 日志检索）。

## 快速开始

需要 Docker（跑 NapCat）、一个 QQ 账号、一个 OpenAI 兼容的 LLM 端点。

```bash
# 1. 启动 NapCat（QQ 登录端），扫描日志中的二维码
echo "PLUMEBOT_SELFID=<你的QQ号>" >> .env      # .env 已被 gitignore，不会入库
docker compose up -d && docker compose logs -f napcat

# 2. 首次运行会自动生成 config.yaml——填入模型 API Key
go build -o bot.exe ./cmd/bot/ && ./bot.exe
```

然后**@ 它**即可开始对话。完整步骤与故障排查见[快速开始](docs/zh/quickstart.md)。

## 文档

- [文档首页](docs/README.md) —— 快速开始、配置全参考、功能指南、插件开发、管理后端
- [English](README.md)
- [架构设计](docs/architecture.md) · [开发规划](docs/roadmap.md) · [管理 API 计划书](docs/admin-web-api-plan.md)

## License

MIT —— 见 [LICENSE](LICENSE)。