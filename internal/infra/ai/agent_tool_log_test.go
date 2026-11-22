package ai

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"plumebot/internal/domain/entity"
	"plumebot/internal/infra/ai/toolmiddleware"
	"plumebot/pkg/logger"
)

func TestZZEinoAgentToolCallLogs(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	baseLogger := zap.New(core)
	t.Cleanup(func() { _ = baseLogger.Sync() })

	var calls atomic.Int32
	params, err := toolutils.GoStruct2ParamsOneOf[echoArgs]()
	if err != nil {
		t.Fatal(err)
	}
	echo := toolutils.NewTool(&schema.ToolInfo{Name: "echo", Desc: "回显", ParamsOneOf: params},
		func(_ context.Context, args echoArgs) (string, error) {
			calls.Add(1)
			return "private-result", nil
		})
	tools := []tool.BaseTool{echo}

	success := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "call-success-1", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"private-argument"}`}},
			{ID: "call-success-2", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"private-argument"}`}},
		}),
		schema.AssistantMessage("已完成", nil),
	}}
	successAgent, err := NewEinoAgent(context.Background(), success, tools, agentCfg(""))
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := successAgent.Generate(agentToolLogContext(baseLogger, "group:42"), []entity.ChatMessage{usrMsg("执行回显")}); err != nil || reply != "已完成" {
		t.Fatalf("正常工具调用失败：reply=%q err=%v", reply, err)
	}

	budget := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-budget", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"private-argument"}`}}}),
		schema.AssistantMessage("预算不足", nil),
	}}
	budgetAgent, err := NewEinoAgent(context.Background(), budget, tools, agentCfg(""), toolmiddleware.ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 40})
	if err != nil {
		t.Fatal(err)
	}
	if reply, err := budgetAgent.Generate(agentToolLogContext(baseLogger, "group:42"), []entity.ChatMessage{usrMsg("执行回显")}); err != nil || reply != "预算不足" {
		t.Fatalf("预算提示路径失败：reply=%q err=%v", reply, err)
	}
	if calls.Load() != 2 {
		t.Fatalf("预算拒绝的工具不应执行，正常并行调用应执行 2 次，实际 %d 次", calls.Load())
	}

	unknown := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-unknown", Function: schema.FunctionCall{Name: "missing_tool", Arguments: `{"text":"private-argument"}`}}}),
	}}
	unknownAgent, err := NewEinoAgent(context.Background(), unknown, tools, agentCfg(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unknownAgent.Generate(agentToolLogContext(baseLogger, "private:7"), []entity.ChatMessage{usrMsg("执行未知工具")}); err == nil {
		t.Fatal("未知工具调用必须继续失败")
	}

	infoLogs := readAgentToolCallLogs(t, logs, "info")
	warnLogs := readAgentToolCallLogs(t, logs, "warn")
	assertAgentToolCallOnce(t, infoLogs, "call-success-1", "group:42", "returned")
	assertAgentToolCallOnce(t, infoLogs, "call-success-2", "group:42", "returned")
	assertAgentToolCallOnce(t, infoLogs, "call-budget", "group:42", "returned")
	assertAgentToolCallOnce(t, warnLogs, "call-unknown", "private:7", "unknown_tool")

	for _, entry := range logs.All() {
		fields := fmt.Sprint(entry.ContextMap())
		if strings.Contains(entry.Message+fields, "private-argument") || strings.Contains(entry.Message+fields, "private-result") {
			t.Fatalf("工具参数或结果正文进入日志：%s %s", entry.Message, fields)
		}
	}
}

func agentToolLogContext(l *zap.Logger, traceID string) context.Context {
	return logger.WithLogger(context.Background(), l.With(zap.String("trace_id", traceID)))
}

func readAgentToolCallLogs(t *testing.T, logs *observer.ObservedLogs, level string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, entry := range logs.All() {
		if entry.Message == "AI工具调用" && entry.Level.String() == level {
			entries = append(entries, entry.ContextMap())
		}
	}
	return entries
}

func assertAgentToolCallOnce(t *testing.T, logs []map[string]any, callID, traceID, outcome string) {
	t.Helper()
	count := 0
	for _, entry := range logs {
		if entry["tool_call_id"] != callID {
			continue
		}
		count++
		if entry["trace_id"] != traceID || entry["outcome"] != outcome {
			t.Errorf("工具调用日志字段不符：%v", entry)
		}
	}
	if count != 1 {
		t.Fatalf("call_id %q 应有 1 条工具调用日志，实际 %d 条：%v", callID, count, logs)
	}
}
