# PlumeBot 文档 / PlumeBot Documentation

**简体中文** · [English](#english)

PlumeBot 是基于 OneBot v11 协议的 QQ「赛博群友」，对接 NapCat，用 Go 实现，单二进制部署。本目录是该项目的完整使用者 / 开发者文档；内部设计以 [architecture.md](architecture.md) 为唯一基准。想参与贡献请先读[贡献指南](../CONTRIBUTING.zh-CN.md)。

---

## 中文文档

| 文档 | 内容 |
|------|------|
| [快速开始](zh/quickstart.md) | 环境要求、启动 NapCat、配置模型、编译运行、验证与故障排查 |
| [配置参考](zh/configuration.md) | `config.yaml` 全字段说明（bot / onebot / control / middleware / llm / tools / agent / admin）与环境变量覆盖 |
| [功能指南](zh/features.md) | 消息管线、三级记忆、触发控制、人格、多模态、群管理、日志与运维 |
| [插件开发指南](zh/plugin-dev.md) | 用 plugin-sdk 编写独立第三方插件：最小示例、协议、部署、调试 |
| [管理后端指南](zh/admin.md) | Web 控制台使用 + 完整 HTTP API 参考 + 配置生效语义 |

## 内部设计

| 文档 | 内容 |
|------|------|
| [architecture.md](architecture.md) | 架构设计与全部决策记录（记忆、压缩、触发、插件协议、日志规范等） |
| [roadmap.md](roadmap.md) | 开发阶段任务台账 + 「待办与遗留事项」B 台账 |
| [admin-web-api-plan.md](admin-web-api-plan.md) | 管理后端接口设计计划书（接口细节、自动建行、缓存失效、校验矩阵） |
| [eino-notes.md](eino-notes.md) | eino v0.8.13 API 速查 |
| [贡献指南](../CONTRIBUTING.zh-CN.md) | 参与贡献：报告 Bug、提交特性/修复、AI 辅助标准、提交信息规范、分层与测试要求 |

---

## English

**English** · [简体中文](#plumebot-文档--plumebot-documentation)

PlumeBot is an AI-driven QQ "cyber community member" built on the OneBot v11 protocol and NapCat, written in Go, shipped as a single static binary. The design record lives in [architecture.md](architecture.md). Want to contribute? Read the [contributing guide](../CONTRIBUTING.md) first.

| Doc | Covers |
|-----|--------|
| [Quick Start](en/quickstart.md) | prerequisites, starting NapCat, configuring a model, building & running, verification & troubleshooting |
| [Configuration](en/configuration.md) | every `config.yaml` field (bot / onebot / control / middleware / llm / tools / agent / admin) and environment overrides |
| [Features Guide](en/features.md) | message pipeline, three-tier memory, trigger control, persona, multi-modal, group management, logging & ops |
| [Plugin Development](en/plugin-dev.md) | writing standalone plugins with the plugin-sdk: minimal example, protocol, deploy, debugging |
| [Admin Backend](en/admin.md) | web console usage + full HTTP API reference + how changes take effect |
| [Contributing](../CONTRIBUTING.md) | issue/PR workflow, AI-assisted standards, commit guidelines, layer rules, testing |

---

Found a bug or missing documentation? Open an issue or pull request. See [README](../README.md) for the project overview.