# 管理后端指南

PlumeBot 内置一个管理 Web 控制台：浏览器访问即可管理群配置、人格、群画像、黑话、成员事实，只读查看运行态与对话历史，并检索日志。同时暴露完整的 HTTP API 供脚本/二次开发使用。

## 1. 总览

- **监听**：默认回环绑定 `127.0.0.1`，不暴露局域网；`admin.enabled: false` 可关闭（仅保留 `/ping` 存活探针）；
- **端口**：默认 `9321`，被占用自动逐次 +1（扫描至 10024）；实际端口打印在启动日志；
- **鉴权**：JWT（HS256）Bearer token；`admin_user` 表为账号唯一事实来源（config 不承载任何账号/密码）；
- **首个管理员**：`admin_user` 表为空时首访进入注册页，第一个到达者创建唯一管理员账号；之后注册入口关闭（4031）；
- **前端**：单页 HTML（原生 fetch，零构建链），随二进制 go:embed。

## 2. 快速使用

```bash
# 1. 启动 bot（默认 admin.enabled=true）
./bot.exe

# 2. 浏览器打开（以启动日志实际端口为准）
#    http://127.0.0.1:9321/

# 3. 首访：注册首个管理员账号 → 登录
```

## 3. 前端页面模块

| Tab | 功能 |
|-----|------|
| 群配置 | 列出已配置群；单群编辑触发模式 / 状态规则参数 / 群管理开关；删除配置行（恢复全局兜底） |
| 人格 | 按 agent 编辑 `system_prompt` / 展示名，**下一条消息即生效** |
| 群画像 | 按群编辑文化 / 话题 / 活跃时段 / 群规 / 氛围标签；删除恢复「无画像」 |
| 黑话审核 | 按群列出 pending / confirmed；确认 / 删除 / 新增（默认 confirmed，写入即注入 prompt） |
| 成员事实 | 按 `group_id + user_id` 查看 / 补记 / 删除事实（Agent 记错的纠错入口） |
| 运行态 | 按会话只读查看精力 / 冷却 / 连续计数等（**只读，无写入端点**） |
| 对话历史 | 活跃会话下拉；窗口消息气泡 + 「更早的对话纪要」（已压缩历史的摘要热链）只读浏览 |
| 日志 | 按 `等级 × 时间段 × trace_id` 组合筛选分页检索（多文件扫描，定价高于配置读写：等级开关只切渲染、「加载更多」按 offset 追加） |

## 4. HTTP API 参考

- **前缀**：`/api/v1`；除 `register` / `login` / `status` 与 `/ping`、`/` 外，一律 `Authorization: Bearer <token>`；
- **响应包络**：

```json
// 成功
{ "code": 0, "message": "ok", "data": { ... } }
// 失败
{ "code": 4001, "message": "中文错误说明", "data": null }
```

- **业务码**：`0` 成功；`4001` 参数错误；`4011` 未认证/凭证错误；`4031` 无权限；`4041` 不存在；`4091` 冲突；`5000` 内部错误；
- **限流**：`/api/v1` 全组每 IP 令牌桶（10/s、burst 30）；`register` / `login` 叠加更紧的 2/s、burst 10；
- **写操作返回**：更新后的资源（`data` 为最新状态）；删操作返回 `{"code":0}`，目标不存在 `4041`。

### 认证域

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/register` | 首个管理员注册（免鉴权）：仅 `admin_user` 空表放行；成功即签 token；已有管理员 → 4031；用户名冲突 → 4091 |
| POST | `/api/v1/auth/login` | 登录；bcrypt 校验，失败统一文案 4011（不区分「用户不存在/密码错误」） |
| GET | `/api/v1/auth/status` | 免鉴权：是否已有管理员（首访前端据此显示注册/登录表单） |
| PUT | `/api/v1/auth/password` | 改密：验旧码 → 更新散列 |
| GET | `/api/v1/auth/me` | 当前登录人（前端刷新校验） |

### 群配置域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/groups/configs` | 所有已配置群列表（含自动建行的群） |
| GET | `/api/v1/groups/:group_id/config` | 单群配置；未配置返回 200 + 全零字段 + `configured:false` |
| PUT | `/api/v1/groups/:group_id/config` | 整行 upsert（幂等）；0/空列 = 走全局；`configured` 恒 true |
| DELETE | `/api/v1/groups/:group_id/config` | 删行 → 恢复全局兜底（**非持久**，见 §5） |

校验（fail-fast）：`mode` 仅 `mention` / `auto`；`group_mgmt_enabled` 仅 0/1；数值参数 `≥0`；`quiet_hours_*` 严格 `"HH:MM"`（可相等 = 空段禁用）。

### 人格域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/personas` | 所有 agent 模板 |
| GET | `/api/v1/personas/:agent` | 单条；不存在 4041 |
| PUT | `/api/v1/personas/:agent` | upsert；**改库即时生效（下一条消息起）** |

校验：`name` trim（可空）；`system_prompt` 非空、长度上限（≤ 20000）。

### 群画像域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/groups/:group_id/profile` | 单群画像；无画像返回零值 + `configured:false`（profile 不自动建行，长期可达） |
| PUT | `/api/v1/groups/:group_id/profile` | upsert `{culture, topics[], active_hours, rules[], atmosphere[]}`；写后自动失效内存缓存 |
| DELETE | `/api/v1/groups/:group_id/profile` | 删除（不存在 4041），同样失效缓存 |

> 关键：群画像走内存缓存，管理面写入时必须先失效缓存（`InvalidateGroupProfile`），否则 prompt 仍读旧画像——该行为由 service/admin 自动处理，无需使用方关心。

### 黑话域（文本一律走 body，避免 URL 转义）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/groups/:group_id/jargons?status=all\|pending\|confirmed` | 按群列表；缺省 `all` |
| POST | `/api/v1/groups/:group_id/jargons` | 添加（默认 `confirmed`，人工添加即人工认可，写入即注入 prompt） |
| DELETE | `/api/v1/groups/:group_id/jargons` | 删除；撤销错误学习/错误确认（不存在 4041） |
| POST | `/api/v1/groups/:group_id/jargons/confirm` | 审核确认 pending → confirmed（确认后下条消息注入 prompt） |

POST / DELETE / confirm 的请求体均为 `{"jargon": "..."}`。

### 成员事实域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/member-facts?group_id=&user_id=` | 查事实；`group_id` 空 = 私聊场景 |
| POST | `/api/v1/member-facts` | 补记/纠错（正常写者仍是 Agent 的 `store_fact`） |
| DELETE | `/api/v1/member-facts?group_id=&user_id=&fact=` | 删除单条（纠错主场景：Agent 记错的事实） |

校验：`user_id`、`fact` 非空，`fact` 长度上限（≤ 500）。

### 会话域（只读）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/sessions` | 活跃会话下拉 `[{key, count, last_ts, last_render}]` |
| GET | `/api/v1/sessions/:session_key/window` | 窗口消息（时间正序）+ 摘要热链 `{items, summaries}` |
| GET | `/api/v1/sessions/:session_key/state` | 运行态只读（精力/冷却/连续计数）；无此会话 4041 |

- `session_key`：群聊 = 群号，私聊 = `private:<QQ号>`（URL 编码）；
- 未知会话的 `window` 返回空数组（非 404）；摘要为「窗口之前已压缩的历史」的展示形态（不区分一级/二级压缩）。

### 日志域

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/logs` | 分页检索 `{items:[{ts,level,message,trace_id,fields}], has_more}` |

参数：`levels`（csv：info,warn,error,debug；缺省全部；`error` 语义含 fatal）、`trace_id`（精确匹配）、`begin`/`end`（RFC3339）、`limit`/`offset`（缺省 200/0，上限 1000）。参数非法一律 400。

## 5. 数据生效语义（务必理解）

| 配置 | 生效方式 |
|------|---------|
| `group_config` / `persona` | **改库即时生效**（每条消息现查，无缓存）——改完下一条消息即按新值 |
| `group_profile` | 改库 **+ 缓存失效**（service 自动做）；配置文件内其余全局参数（`control.*`、模型、中间件）需**重启** |
| 全局 `config.yaml` | 需重启（无热更新）；管理面只管 JSON 中能查到的（persona/群画像等） |

**`group_config` 自动建行**：群首次被 bot 触达时按**当时的全局生效值**快照自动落一行。含义：

- 此后该群**独立于全局**——`config.yaml` 的 `control.mode` / `control.state` 变更不再影响它，需在该群行上改；
- `configured:false` 只在该群**尚未被 bot 触达**时可达；
- DELETE 删行**非持久**——该群下一条消息会按当时的全局值重新建行；「永久回到走全局」暂无对应操作（此后再手工删除一次即可）。

## 6. 安全

- **回环绑定**：默认仅本机；远程管理需自行配监听地址 + 内网 / 反向代理 TLS；
- **账号**：密码只存 bcrypt 散列；登录失败统一文案；登录/注册每 IP 限流防爆破；改密需旧码；
- **密钥**：`jwt_secret` 空则首启自动生成并持久化 `data/admin_jwt_secret`（0600）；日志绝不打印 token/密码/散列；
- **首次注册门控**：首账号创建后注册入口永久关闭（fail-closed）；
- **fail-fast 校验**：非法入参一律 4xx，不落脏数据（与运行期「非法保留默认」的容错语义刻意区分）；
- **审计**：写操作统一 `admin config changed` 审计（操作者/资源/目标/IP）；不暴露发送消息/群管理等「行动型」端点（那些走 bot 侧 Sender / GroupManager 通道）。

## 7. 参考

- 接口设计与契约细节（自动建行、缓存失效、校验矩阵）：[admin-web-api-plan.md](../admin-web-api-plan.md)
- 日志规范与 trace_id：架构 [§17](../architecture.md)