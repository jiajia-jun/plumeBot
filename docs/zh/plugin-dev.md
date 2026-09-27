# 插件开发指南

## 插件是什么

PlumeBot 的插件是为 bot 扩展**命令**（以 `/` 开头的消息走插件分发）的第三方程序。设计上有三条硬约束：

- **独立进程**：插件编译成独立 exe，经 go-plugin（net/rpc + gob）与宿主 stdio 通信——跨平台、进程隔离（插件崩溃不影响 bot）、热重载（重启插件子进程即生效）；
- **零权限、只声明意图**：插件进程**碰不到 OneBot API**，只能返回结构化结果 `PluginResult{Reply, Actions}`——回复与群管理动作都由宿主唯一执行；
- **只依赖 SDK**：插件用独立 module [`github.com/plumebot/plumebot-sdk`](https://github.com/plumebot/plumebot-sdk) 编写，不 import 宿主 internal 包，第三方可独立开发。

仓库根目录 `plugins/example/` 有一份可直接编译的完整示例（`echo` 命令）。

## 最小插件

一个最小插件只需三步：**依赖 SDK → 实现 `Execute` → 调 `Serve`**。

```go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/plumebot/plumebot-sdk/plugin"
)

type hello struct{}

func (h *hello) Execute(_ context.Context, req plugin.PluginRequest) (plugin.PluginResult, error) {
	if req.Command != "hello" {
		return plugin.PluginResult{}, fmt.Errorf("unknown command: %s", req.Command)
	}
	return plugin.PluginResult{
		Reply: &plugin.Reply{
			Segments: []plugin.Segment{{
				Kind: plugin.SegmentKindText,
				Text: "你好！参数：" + strings.Join(req.Args, " "),
			}},
		},
	}, nil
}

func main() {
	plugin.Serve(&hello{})
}
```

`go.mod`：

```go
module example

go 1.26

require github.com/plumebot/plumebot-sdk v0.1.0
```

## 编译与部署

```bash
go build -o hello.exe .        # 交叉编译同样可行（纯 Go）
mkdir -p plugins/hello
mv hello.exe plugins/hello/
```

宿主扫描 `plugins/` 目录，读取每个子目录下的 `plugin.json` 元数据：

```json
{
  "name": "hello",
  "path": "hello.exe",
  "commands": ["hello"],
  "description": "一个示例插件"
}
```

| 字段 | 说明 |
|------|------|
| `name` | 插件名（目录名通常同名） |
| `path` | 可执行文件相对路径 |
| `commands` | 该插件声明的命令（不带 `/`）；命令分发据此路由 |
| `description` | 展示用描述 |

把插件目录放进 bot 运行时的 `plugins/` 下并重启 bot（或重建启动流程），即可被自动发现。

## 命令分发

- 群 / 私聊消息以 `/` 开头即进入命令分支（如 `/echo hello` → 命令 `echo`，参数 `["hello"]`）；
- 宿主导航到 `plugin.json` 声明的 `commands`，再调用对应插件的 `Execute`，`PluginRequest.Args` 携带参数；
- 命令分支短路回复流程：插件处理后不再走 Agent。

## 协议参考

`github.com/plumebot/plumebot-sdk/entity` 定义协议 wire 类型，`.../plugin` 提供接线（`Serve` / `NewClient`）。常用类型：

```go
type PluginRequest struct {
	Command string
	Args    []string
	Session struct {
		GroupID   string // 空 = 私聊
		UserID    string
		MessageID string
	}
}

type PluginResult struct {
	Reply   *Reply         // 可空；宿主校验后经 domain.Sender 发送
	Actions []GroupAction  // 可空；宿主校验后经 domain.GroupManager 执行
}

type Reply struct {
	Quote    bool        // 引用触发消息
	At       string      // "" | "sender" | 具体 user_id
	Segments []Segment   // 多段混排：文本 / 图片 / 表情
}

type Segment struct {
	Kind  SegmentKind   // SegmentKindText | SegmentKindImage | SegmentKindFace
	Text  string
	Image *ImageRef     // 图片引用
}

type ImageRef struct {
	Source ImageSource  // ImageSourcePath | ImageSourceURL（本地路径 / 网络 URL）
	Value  string
}

type GroupAction struct {
	Op       GroupOp    // GroupOpMute | GroupOpUnmute | GroupOpKick | GroupOpSetCard
	Target   string     // 目标用户 ID
	Duration int64      // 禁言时长（秒），仅 Mute 生效
	Card     string     // 名片，仅 SetCard 生效
}
```

### 宿主校验（`ValidatePluginResult`）

宿主收到结果会先校验再执行，以下情况直接拒绝：

- 未知的 `SegmentKind` / `GroupOp` / `ImageSource` 枚举值；
- `Reply` 与 `Actions` 均为空（无内容可执行）；
- 图片段缺少地址等必填字段。

回复经 `domain.Sender` 发送（支持文本 / 图片 / 表情 / 引用 / @）；群管理动作经 `domain.GroupManager` 执行，与 AI 工具走**同一套护栏**：per-group 开关 + 管理员校验（fail-closed）+ 禁言时长钳制 30 天。插件声明的动作不代表一定能执行——**护栏在宿主唯一一处收敛**。

## 调试与运维

- **日志**：插件被子进程拉起，宿主日志（`info.log` / `warn.log`）会记录命令分发与插件回复/执行结果；插件自身 stdout/stderr 不受宿主管控，可自行落自己的日志文件；
- **崩溃隔离**：插件进程崩溃不影响 bot，宿主可检测进程退出（自动重启属后端能力，见 roadmap B-018/B-019）；
- **热重载**：改插件代码 → 重新编译 exe → 重启 bot（或按 B-018 实现 mtime 轮询自动重启插件进程）。

## 发布说明

SDK `github.com/plumebot/plumebot-sdk` 当前版本 `v0.1.0`，以远程 module 形式提供（宿主 `go.mod` 直接 require）。插件只需在各自 `go.mod` 声明该依赖，无需与 bot 主程序同版本编译。

示例源码：`plugins/example/`（含 `echo` 命令、多段回复、图片段与群管理动作演示）。