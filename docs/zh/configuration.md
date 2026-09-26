# 配置参考

## 配置文件

- **位置**：运行目录下的 `config.yaml`（本机文件，已被 .gitignore 忽略，不含密钥，不会入库）；
- **生成**：首次运行若不存在，则由嵌入二进制的默认模板（`pkg/config/config.default.yaml`）自动写入并加载；
- **模板同步**：主程序更新后，`config.yaml` 与 `config.default.yaml` 可能产生新字段——模板双处同步靠人工维护（见 `go.mod`/`CLAUDE.md` 约定）；
- **兜底**：空值 / 缺省字段不写入配置，由各消费方按代码默认兜底（下文各表的默认值即消费方兜底值）。

## 环境变量覆盖

仅**敏感 / 机器相关**字段支持环境变量覆盖（不做通用 env 读取）：

| 变量 | 覆盖字段 | 说明 |
|------|---------|------|
| `PLUMEBOT_APIKEY_<模型名大写>` | 对应模型条目的 `api_key` | 如 `name: chat` → `PLUMEBOT_APIKEY_CHAT`；非空时优先于文件值；`name` 为空的条目不参与 |
| `PLUMEBOT_SELFID` | `bot.self_id` | 与 docker-compose 的 `ACCOUNT` 共用同一来源 |

在 shell 中：

```bash
export PLUMEBOT_APIKEY_CHAT=sk-xxx
export PLUMEBOT_SELFID=10001
./bot.exe
```

## 配置段总览

| 段 | 作用 | 生效方式 |
|----|------|---------|
| `bot` | bot 基本信息 | 重启 |
| `onebot` | NapCat 连接 | 重启 |
| `log` | 日志级别 | 重启 |
| `control` | 触发模式 + 状态规则全局默认 | 重启（`group_config` 建行后独立于全局，见 [功能指南](features.md)） |
| `middleware` | 限流 + 敏感词 | 重启 |
| `llm` | 模型条目 / 对话与视觉模型引用 / prompt 预算 | 重启 |
| `tools` | 启用的工具列表 | 重启 |
| `agent` | agent 三要素（元数据 + 全局兜底人设） | 重启 |
| `admin` | 管理后端服务配置 | 重启 |

## `bot`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `name` | `PlumeBot` | bot 展示名（QQ 侧）；空 → `PlumeBot` |
| `self_id` | 空 | bot 自身 QQ 号；可用环境变量 `PLUMEBOT_SELFID` 覆盖 |

## `onebot`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `ws_url` | `ws://127.0.0.1:3001` | NapCat「WebSocket 服务器」地址（正向 WS）；空 → 默认值 |
| `access_token` | `""` | NapCat 若设置了访问令牌则填写 |

## `log`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `level` | `info` | 日志门控：`debug` / `info` / `warn` / `error`；`fatal.log` 恒开 |

日志按精确级别分文件落 `~/.plumebot/logs/`（`debug/info/warn/error/fatal.log` + gin 访问 `gin.log`），详见 [功能指南 · 日志与运维](features.md)。

## `control`

触发模式与状态规则的 **全局默认值**。`mode` 默认 `mention`；`state` 各参数 0/空 = 用代码默认（如下表）。

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `mode` | `mention` | `mention`（仅被 @ 或私聊时回复）\| `auto`（普通消息由 Agent 自主判断是否加入） |
| `state.energy_max` | 100 | 精力上限 |
| `state.energy_cost` | 10 | 每次回复消耗 |
| `state.energy_recover` | 5 | 精力恢复（点/分钟） |
| `state.energy_threshold` | 20 | 精力低于此值不主动说话（@ 除外） |
| `state.cooldown_seconds` | 60 | 两次主动回复最小间隔（秒） |
| `state.consecutive_limit` | 5 | 连续回复上限，达此值进入强制休息 |
| `state.rest_seconds` | 300 | 强制休息时长（秒） |
| `state.quiet_hours_start` | `23:00` | 深夜静默起始 `"HH:MM"` |
| `state.quiet_hours_end` | `07:00` | 深夜静默结束 `"HH:MM"`（支持跨午夜） |
| `state.short_message_chars` | 4 | 短消息忽略阈值（内容短于 N 字不触发） |

> 这些只是每组全局默认。每个群被 bot 首次触达时会按当时的全局生效值**自动建行**到 `group_config`，此后该群**独立于全局**（改 `config.yaml` 的 `control.*` 不再影响它，需在管理控制台按群修改）。

## `middleware`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `rate_limit.rate` | 2 | 令牌补充速率（个/秒）；≤0 → 2 |
| `rate_limit.burst` | 20 | 令牌桶容量（允许的突发消息数）；≤0 → 20 |
| `rate_limit.max_wait_seconds` | 10 | 等待令牌上限；超时回复固定文案并丢弃；≤0 → 10 |
| `sensitive_words` | `[]` | 敏感词表；命中回复「我拒绝回答」并丢弃；空数组 = 不过滤（Aho-Corasick 自动机匹配） |

## `llm`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `timeout_seconds` | 60 | 单次 LLM 调用超时（秒）；≤0 → 60 |
| `native_multimodal` | `false` | 原生多模态模式（阶段 3，字段占位，默认关） |
| `models` | — | 模型条目数组（见下） |
| `models[].name` | — | 用户取名，被 `chat_model` / `vision_model` 引用 |
| `models[].provider` | `openai` | 供应商：`openai`（OpenAI 兼容）；空 → `openai` |
| `models[].base_url` | `https://api.openai.com/v1` | OpenAI 兼容端点 |
| `models[].api_key` | `""` | API Key；本地模型（Ollama）可留空；可用 `PLUMEBOT_APIKEY_<模型名大写>` 覆盖 |
| `models[].model` | — | 模型名；**空 → 启动报错**（不可猜测） |
| `models[].temperature` | 不传 | 采样温度；缺省不传（用模型默认） |
| `models[].max_tokens` | 不传 | 最大输出 token；≤0 → 不传 |
| `chat_model` | `models[0].name` | 对话模型条目引用；空 → 第一条 |
| `vision_model` | `""` | 图片描述模型条目引用；空 = 图片描述关闭 |
| `prompt.facts_per_member` | 3 | 窗口内每成员事实条数注入上限；≤0 → 3 |
| `prompt.jargon_cap` | 20 | confirmed 黑话条数注入上限；≤0 → 20 |
| `prompt.describe_recent_rounds` | 5 | 只描述窗口最近 N 轮消息中的图片；≤0 → 5 |
| `prompt.describe_per_turn_cap` | 10 | 每条消息图片描述条数上限；≤0 → 10 |

示例：启用一个视觉模型用于图片描述需要同时在 `models` 里加一条**和**设 `vision_model`：

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

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `enabled` | — | 启用的工具名列表；空 = 不注册任何工具 |

内置工具：

- 记忆更新：`store_fact` / `learn_jargon` / `forget_fact`（Agent 在对话中经 tool calling 写 `member_facts` / `group_jargon`）；
- 群管理：`group_mute` / `group_unmute` / `group_kick` / `group_set_card`（受 `group_config.group_mgmt_enabled` 开关与管理员校验双重护栏，见 [功能指南](features.md)）。

## `agent`

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `name` | `PlumeBot` | agent 标识（adk 元数据，multi-agent 路由用）；空 → `PlumeBot` |
| `description` | 默认描述 | 能力描述（adk 元数据） |
| `system_prompt` | 默认人设 | 全局兜底人设；仅当 `persona` 表无该 agent 对应模板时使用（见 [功能指南 · 人格](features.md)）。一般建议在管理控制台的「人格」页维护人设，改库即时生效 |

## `admin`

管理后端服务配置（不承载任何账号 / 密码——账号唯一来源是数据库 `admin_user` 表）。

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `enabled` | `true` | 管理 API + 前端页开关；`false` → 仅保留 `/ping` 存活探针 |
| `port` | `9321` | gin 监听起始端口；被占用逐次 +1（扫描至 10024），全占用仅告警 |
| `jwt_secret` | 留空 | JWT 签名密钥；空 → 首启自动生成并持久化 `data/admin_jwt_secret`（0600），跨重启 token 保持有效 |
| `token_ttl_seconds` | `86400` | 登录 token 有效期（秒）；≤0 → 86400 |

详见 [管理后端指南](admin.md)。