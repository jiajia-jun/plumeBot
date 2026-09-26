# 参与 PlumeBot 贡献

本文是 PlumeBot 的贡献指南。英文版见 [CONTRIBUTING.md](CONTRIBUTING.md)。

开始之前，请先阅读本仓库的两份纲领性文档：

- [CLAUDE.md](CLAUDE.md) —— **开发契约**：技术栈白名单（含「明确不使用」清单）、分层规则、目录规范、以及「一次只完成一个任务」的原则；
- [docs/roadmap.md](docs/roadmap.md) —— **任务台账**：按阶段推进，含「待办与遗留事项」B 台账。**不要提前实现**属于后续阶段的功能。

## 报告 Bug

如果只是提问、或不确定是否为 bug，请先发起 Discussion 而非 Issue。

提交 Issue 时请尽量包含：

- PlumeBot 版本（运行中的 commit，如 `git log -1 --oneline`），以及 **隐去所有 `api_key` 后的 `config.yaml`**（或至少 `llm.models` 的条目形状，不含密钥）；
- 操作系统与架构（如 Windows 11 x86_64、Linux x86_64）；
- 复现步骤：做了什么、期望什么、实际怎样；
- 关键日志（`~/.plumebot/logs/`）：每条消息恰好一条**入口行**（`收到消息`）与一条**结局行**（`消息结局`）。如可能，附上出问题消息的 entry/outcome 对及周边 `warn.log` / `error.log`；若失败在某条请求链内，附上 `trace_id`（`group:<群号>` / `private:<QQ>`），维护者可以此串联整条链路。

## 提交新功能或 Bug 修复

大功能请先开 Issue 讨论。小修复与纯文档改动可直接提 Pull Request。

1. **先读契约。** 从 [CLAUDE.md](CLAUDE.md) 与 [文档首页](docs/README.md) 开始；查 [roadmap](docs/roadmap.md) 确认该工作未被跟踪、且不属于后续阶段。

2. **Fork 并克隆。**

   ```console
   git clone https://github.com/plumebot/plumeBot.git
   cd plumeBot
   git remote rename origin upstream
   git remote add origin git@github.com:YOURUSER/plumeBot.git
   ```

3. **开分支动手。**

   ```console
   go build ./...
   git checkout -b my-feature
   ```

   先看下面的[代码布局](#代码布局与架构)与[分层规则](#分层规则)。

4. **提交前先测。** 小修复跑包级测试即可，大改动跑[全量测试](#测试)：

   ```console
   go test ./...
   ```

5. 确认你的改动同时包含：
   - 新行为对应的单测；
   - 文档更新（双语，见[编写文档](#编写文档)）；
   - 符合[提交信息规范](#提交信息)的 commit。

6. 推送并开 PR：

   ```console
   git push -u origin my-feature
   ```

你的变更会被 review。若被要求调整，在同一分支继续提交并推送。你可能也会被要求先 [rebase 到最新 main](#保持分支同步) 或 [squash 提交](#合并提交)。

## AI 辅助贡献

本项目欢迎 AI 编程助手（Claude Code、Codex、Cursor、Gemini CLI 等）——事实上 `CLAUDE.md` 就是为「把工具指向它就能产出地道 PlumeBot 代码」而写的。对执行方能而言它即是规范：分层、一次一个任务、不越阶段实现。

无论是否使用工具，每条 PR 的标准一致：**你对自己提交的代码负责。** 开 PR 前请确认：

- 你理解每一行改动、能解释其正确性——reviewer 提问时你能不回去问工具就回答；
- 你确实编译并运行过：至少 `go build ./...` 与 `go test ./...` 通过；涉及配置/迁移的改动，还需确认两个配置文件保持同步（见[配置改动](#配置改动)）；
- 改动是经你验证能解决问题的真实修复/特性，而不是貌似合理的一猜。未经验证、编译不过、测试不通过、发明不存在的 API、或做不出描述声称效果的 AI 生成的 PR，会浪费维护者时间，很可能被直接关闭；
- 你修剪过注释：AI 工具倾向加「复述代码」的多余注释，请与周边代码的注释密度保持一致。

一句话：AI 是帮助你贡献的工具，不是替你理解与测试的替代品。

## 使用 Git 与 GitHub

### 提交

```console
git checkout my-feature
git status          # 查看新增/改动文件
git add FILENAME    # 逐个暂存
git commit          # 遵循提交信息规范
```

改最近一次提交：`git commit --amend`。

### 改写后推送

若已推送的提交被 amend / rebase 过，用强制推送替换：

```console
git push --force origin my-feature
```

改写已推送历史前最好先与协作者同步。

### 保持分支同步

```console
git fetch upstream
git checkout main && git merge --ff-only
git checkout my-feature && git rebase main
git push --force origin my-feature
```

### 合并提交

```console
git reset --soft HEAD~N    # 撤销最近 N 个提交，改动保留在暂存区
git commit
```

更复杂的场景可用 `git rebase -i main`。

## 提交信息

使用 [Conventional Commits](https://www.conventionalcommits.org/)：单行主题

```text
<type>(<scope>): <summary>
```

- `type`：`feat` / `fix` / `refactor` / `docs` / `test` / `perf` / `chore`；
- `scope`：改动所属的包或域，取自本项目：`domain`、`onebot`、`memory`、`control`、`admin`、`ai`、`sqlite` 等；
- `summary`：一句话概括**对使用者可见**的行为变化（仓库惯例为中文主题，英文亦可）。

修复某个 issue 时在信息中加入 `Fixes #123`。

**不要**在提交信息里追加 `Co-Authored-By` / `Co-authored-by` 或任何署名尾注。

首行应可独立成文——项目的变更记录就是由这些主题拼出来的。需要更多上下文时，空一行写正文，解释**为什么**需要这个改动，并对比改动前后的行为。

## 代码布局与架构

```
cmd/             入口；依赖注入（仅 main 包）
internal/
  domain/        接口 + 实体 + 哨兵错误（entity/errors.go）；零第三方 import
    entity/      公共实体（Message、GroupConfig、...）+ 错误
  service/       编排层；只依赖 domain 接口，绝不 import infra
  handler/       薄胶水；消息/通知事件 → service
  infra/         domain 接口的实现（onebot、ai、sqlite、logfile）
plugin-sdk/      （远程 module github.com/plumebot/plumebot-sdk）独立插件 SDK
pkg/             可复用工具（config、logger、jwt、ahocorasick、...）
docs/            双语文档（zh/ + en/）、架构、roadmap、计划书
plugins/         运行时第三方插件（独立进程）
```

完整布局见 [CLAUDE.md §4](CLAUDE.md) 与 [docs/features.md §9](docs/zh/features.md)。

### 分层规则

1. `domain` **不 import 任何第三方库**（仅标准库）；
2. `service` **不 import `infra`** —— 只面向 domain 接口编排；
3. 接口定义在 `domain`、实现在 `infra`；依赖在 `cmd` 经构造函数注入（无全局变量）；
4. 哨兵/校验错误统一归 `internal/domain/entity/errors.go`——不要在 infra 包里自造哨兵；
5. SQL：DML 一律提炼为各 infra 包 `queries.go` 的包级 `const`；DDL 进 `migrations/*.sql`（`//go:embed` 加载），经 `schema_migrations` 版本化——**绝不原地改旧迁移**，新增列一律新建 `00N_*.sql`；
6. **不越阶段实现**：若功能属于后续阶段或「后置 / B 台账」项，先讨论再接。

### 日志

日志规范（每条消息恰好一入口一结局、等级语义、审计收敛、`trace_id`）见 [architecture.md §17](docs/architecture.md)。新增日志遵循它；同一事实的审计行只落在唯一一处。

## 测试

在项目根目录执行：

```console
go build ./...
go vet ./...
go test ./...
```

- 测试只用 Go 标准库 `testing`——**不用 testify**；
- 多数包已有单测（清单见 [CLAUDE.md §0](CLAUDE.md)）；改动应带上受影响包的单测；
- 真实 LLM 冒烟测试以 `PLUMEBOT_TEST_LLM=1` 门控（需真实端点），不在默认套件中；
- 完整端到端稳定性（内存/goroutine 泄漏、API 频控）为待实现阶段（P6-003），本期不在默认流程。

## 新增或更新依赖

技术栈是**封闭白名单**——见 [CLAUDE.md §3](CLAUDE.md) 的技术栈与「明确不使用」清单（切记：不用 `stretchr/testify`、不引入 cgo 依赖库、SQLite 用 `modernc.org/sqlite`、不接外部向量库 / Redis / 消息队列）。新增前先确认候选符合。

```console
go get github.com/example/new_dependency
```

把生成的 `go.mod` 与 `go.sum` 与你的其他改动**放在同一次 commit** 提交。

## 编写文档

- 文档**双语**：改 `docs/zh/*` 必须同步 `docs/en/*`（反之亦然），且在同一 PR 内提交；
- `docs/architecture.md` 是设计决策的唯一基准——代码遵循它，而非相反；
- `docs/roadmap.md` 是任务台账——**新增工作加行，完成即删行**；
- `README.md` / `README.zh-CN.md` 只是**门面**：精炼介绍 + 链接，细节放 `docs/`（如 `docs/zh|en/` 下的五页站点）；
- 插件 SDK 的变更在 `github.com/plumebot/plumebot-sdk` 仓库文档化，`docs/zh|en/plugin-dev.md` 引用之。

### 配置改动

新增配置字段必须**双处同步**（义务 B-006）：`pkg/config/config.default.yaml`（嵌入模板）**和**根 `config.yaml`（本地文件——注意 `config.yaml` 已被 gitignore，入库的只有默认模板）。同步更新[配置参考](docs/zh/configuration.md)。

## 发版

暂未正式化。到目前为止的发布是手工构建（`build.bat` 产出 `bot.exe` / `bot-linux`）；为仓库打普通版本号 tag 即可（无需 `v` 前缀），并对照 [roadmap](docs/roadmap.md)。如需正式的发版机制，可开 issue 讨论。

## License

提交即视为同意你的贡献按项目的 [MIT license](LICENSE) 授权。