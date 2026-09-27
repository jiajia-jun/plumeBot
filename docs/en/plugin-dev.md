# Plugin Development Guide

## What a plugin is

PlumeBot plugins extend the bot with **commands** (messages starting with `/` route to plugin dispatch). Three hard constraints shape them:

- **Separate process**: a plugin is compiled into a standalone executable that talks to the host over stdio via go-plugin (net/rpc + gob) — cross-platform, process-isolated (a crashing plugin never takes down the bot), hot-reloadable;
- **Zero permission, intent-only**: a plugin process **never touches the OneBot API**; it only returns a structured `PluginResult{Reply, Actions}` — the host is the sole executor for replies and group actions;
- **SDK only**: plugins are written against the standalone [`github.com/plumebot/plumebot-sdk`](https://github.com/plumebot/plumebot-sdk) module, never against host `internal` packages — third parties develop independently.

The repo ships a complete, buildable example at `plugins/example/` (an `echo` command).

## A minimal plugin

Three steps: **depend on the SDK → implement `Execute` → call `Serve`**.

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
				Text: "Hello, args: " + strings.Join(req.Args, " "),
			}},
		},
	}, nil
}

func main() {
	plugin.Serve(&hello{})
}
```

`go.mod`:

```
go 1.26

require github.com/plumebot/plumebot-sdk v0.1.0
```

## Build & deploy

```bash
go build -o hello.exe .        # cross-compilation works too (pure Go)
mkdir -p plugins/hello
mv hello.exe plugins/hello/
```

The host scans the `plugins/` directory and reads the `plugin.json` metadata in each subdirectory:

```json
{
  "name": "hello",
  "path": "hello.exe",
  "commands": ["hello"],
  "description": "an example plugin"
}
```

| Field | Description |
|-------|-------------|
| `name` | plugin name (usually the same as the directory) |
| `path` | relative path to the executable |
| `commands` | the commands this plugin declares (without `/`); used to route command dispatch |
| `description` | display description |

Drop the plugin directory under the bot's runtime `plugins/` and (re)start the bot to discover it.

## Command dispatch

- A group / private message starting with `/` enters the command branch (e.g. `/echo hello` → command `echo`, args `["hello"]`);
- The host routes to the plugin whose `plugin.json` declares that command and calls `Execute` with `PluginRequest.Args`;
- The command branch short-circuits the reply pipeline: after a plugin runs, the agent is not consulted.

## Protocol reference

`github.com/plumebot/plumebot-sdk/entity` defines the wire types; `.../plugin` provides the wiring (`Serve` / `NewClient`). Common types:

```go
type PluginRequest struct {
	Command string
	Args    []string
	Session struct {
		GroupID   string // empty = private chat
		UserID    string
		MessageID string
	}
}

type PluginResult struct {
	Reply   *Reply         // optional; validated, then sent via domain.Sender
	Actions []GroupAction  // optional; validated, then executed via domain.GroupManager
}

type Reply struct {
	Quote    bool        // quote the triggered message
	At       string      // "" | "sender" | a concrete user_id
	Segments []Segment   // mixed segments: text / image / face
}

type Segment struct {
	Kind  SegmentKind   // SegmentKindText | SegmentKindImage | SegmentKindFace
	Text  string
	Image *ImageRef     // image reference (required for image segments)
}

type ImageRef struct {
	Source ImageSource  // ImageSourcePath | ImageSourceURL (local path / network URL)
	Value  string
}

type GroupAction struct {
	Op       GroupOp    // GroupOpMute | GroupOpUnmute | GroupOpKick | GroupOpSetCard
	Target   string     // target user ID
	Duration int64      // ban duration in seconds; mute only
	Card     string     // nickname/card; set-card only
}
```

### Host validation (`ValidatePluginResult`)

The host validates before executing and rejects:

- unknown `SegmentKind` / `GroupOp` / `ImageSource` enum values;
- both `Reply` and `Actions` empty (nothing to execute);
- image segments missing their address or other required fields.

Replies are sent via `domain.Sender` (text / image / face / quote / @); group actions run through `domain.GroupManager` behind the **same guards as AI tools**: per-group switch + admin check (fail-closed) + 30-day mute clamp. A plugin-declared action is not guaranteed to execute — **the guards live in one place, on the host**.

## Debugging & operations

- **Logs**: the host records command dispatch and plugin reply/execution results in `info.log` / `warn.log`; plugin stdout/stderr is not managed by the host — log to your own file if needed;
- **Crash isolation**: a plugin crash never affects the bot; the host can detect process exit (auto-restart is a later backend capability, see roadmap B-018/B-019);
- **Hot reload**: change code → recompile the exe → restart the bot (or later use mtime polling to restart just the plugin process).

## Release notes

The SDK `github.com/plumebot/plumebot-sdk` is a remote module at `v0.1.0` (the host's `go.mod` requires it directly). Plugins only declare that dependency in their own `go.mod` — they never need to compile against the same Go version or module graph as the host.

Example source: `plugins/example/` (the `echo` command with mixed segments, an image segment, and a group-management action).