package toolmiddleware

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"plumebot/pkg/logger"
)

func TestToolCallLoggerCoversEndpointKinds(t *testing.T) {
	buf, baseLogger := initToolLogger(t)
	ctx := toolLogContext(baseLogger, "group:42")
	middleware := NewToolCallLogger()

	invokeInput := toolInput("call-invoke")
	invoke := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return &compose.ToolOutput{Result: "private-result"}, nil
	})
	if _, err := invoke(ctx, invokeInput); err != nil {
		t.Fatal(err)
	}

	streamInput := toolInput("call-stream")
	stream := middleware.Streamable(func(context.Context, *compose.ToolInput) (*compose.StreamToolOutput, error) {
		return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{"private-result"})}, nil
	})
	streamOutput, err := stream(ctx, streamInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := streamOutput.Result.Recv(); err != nil {
		t.Fatal(err)
	}
	streamOutput.Result.Close()

	enhancedInput := toolInput("call-enhanced")
	enhanced := middleware.EnhancedInvokable(func(context.Context, *compose.ToolInput) (*compose.EnhancedInvokableToolOutput, error) {
		return &compose.EnhancedInvokableToolOutput{Result: textToolResult("private-result")}, nil
	})
	if _, err := enhanced(ctx, enhancedInput); err != nil {
		t.Fatal(err)
	}

	enhancedStreamInput := toolInput("call-enhanced-stream")
	enhancedStream := middleware.EnhancedStreamable(func(context.Context, *compose.ToolInput) (*compose.EnhancedStreamableToolOutput, error) {
		return &compose.EnhancedStreamableToolOutput{Result: schema.StreamReaderFromArray([]*schema.ToolResult{textToolResult("private-result")})}, nil
	})
	enhancedStreamOutput, err := enhancedStream(ctx, enhancedStreamInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enhancedStreamOutput.Result.Recv(); err != nil {
		t.Fatal(err)
	}
	enhancedStreamOutput.Result.Close()

	entries := toolLogEntries(t, buf, "info")
	assertCallIDsOnce(t, entries, "call-invoke", "call-stream", "call-enhanced", "call-enhanced-stream")
	for _, entry := range entries {
		if entry["trace_id"] != "group:42" || entry["tool_name"] != "echo" || entry["outcome"] != "returned" {
			t.Errorf("调用日志字段不符：%v", entry)
		}
	}
	assertNoToolPayload(t, buf)
}

func TestToolCallLoggerLogsErrorsCancellationAndPanic(t *testing.T) {
	buf, baseLogger := initToolLogger(t)
	ctx := toolLogContext(baseLogger, "private:7")
	middleware := NewToolCallLogger()

	failed := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return nil, errors.New("endpoint failed")
	})
	if _, err := failed(ctx, toolInput("call-error")); err == nil {
		t.Fatal("期望工具错误透传")
	}

	cancelled := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return nil, context.Canceled
	})
	if _, err := cancelled(ctx, toolInput("call-cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误未透传：%v", err)
	}

	panicked := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		panic("private-panic")
	})
	recovered := false
	func() {
		defer func() { recovered = recover() != nil }()
		_, _ = panicked(ctx, toolInput("call-panic"))
	}()
	if !recovered {
		t.Fatal("panic 应记录后继续向上传播")
	}

	entries := toolLogEntries(t, buf, "warn")
	assertCallIDsOnce(t, entries, "call-error", "call-cancelled", "call-panic")
	want := map[string]string{"call-error": "error", "call-cancelled": "cancelled", "call-panic": "panic"}
	for _, entry := range entries {
		callID := entry["tool_call_id"].(string)
		if entry["trace_id"] != "private:7" || entry["outcome"] != want[callID] {
			t.Errorf("失败日志字段不符：%v", entry)
		}
	}
	assertNoToolPayload(t, buf)
}

func TestToolCallLoggerObservesOutputBudgetRejection(t *testing.T) {
	buf, baseLogger := initToolLogger(t)
	ctx := toolLogContext(baseLogger, "group:9")
	middleware := NewToolCallLogger()
	limiter := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 40})
	called := false
	base := func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		called = true
		return &compose.ToolOutput{Result: "must-not-run"}, nil
	}

	ctx = WithToolOutputBudget(ctx, ToolOutputLimitConfig{TotalChars: 40})
	endpoint := middleware.Invokable(limiter.Invokable(base))
	output, err := endpoint(ctx, toolInput("call-budget-notice"))
	if err != nil || output.Result != budgetMessage || called {
		t.Fatalf("预算提示路径不符：output=%+v called=%v err=%v", output, called, err)
	}

	limitedCtx := toolLogContext(baseLogger, "group:9")
	limitedCtx = WithToolOutputBudget(limitedCtx, ToolOutputLimitConfig{TotalChars: 10})
	limitedLimiter := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 10})
	limitedEndpoint := middleware.Invokable(limitedLimiter.Invokable(base))
	if _, err := limitedEndpoint(limitedCtx, toolInput("call-budget-error")); err == nil {
		t.Fatal("预算连提示都放不下时应返回错误")
	}
	if called {
		t.Fatal("预算拒绝时不应执行工具")
	}

	assertCallIDsOnce(t, toolLogEntries(t, buf, "info"), "call-budget-notice")
	assertCallIDsOnce(t, toolLogEntries(t, buf, "warn"), "call-budget-error")
}

func TestHandleUnknownToolLogsAndReturnsError(t *testing.T) {
	buf, baseLogger := initToolLogger(t)
	ctx := toolLogContext(baseLogger, "private:11")
	if result, err := HandleUnknownTool(ctx, "missing_tool", `{"value":"private-argument"}`); err == nil || result != "" {
		t.Fatalf("未知工具必须维持失败语义：result=%q err=%v", result, err)
	}

	entries := toolLogEntries(t, buf, "warn")
	if len(entries) != 1 {
		t.Fatalf("未知工具应恰好记录一次，实际 %d 条：%v", len(entries), entries)
	}
	if entries[0]["tool_name"] != "missing_tool" || entries[0]["trace_id"] != "private:11" || entries[0]["outcome"] != "unknown_tool" {
		t.Fatalf("未知工具日志字段不符：%v", entries[0])
	}
	assertNoToolPayload(t, buf)
}

func toolInput(callID string) *compose.ToolInput {
	return &compose.ToolInput{
		Name:      "echo",
		Arguments: `{"value":"private-argument"}`,
		CallID:    callID,
	}
}

func initToolLogger(t *testing.T) (*observer.ObservedLogs, *zap.Logger) {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	l := zap.New(core)
	t.Cleanup(func() { _ = l.Sync() })
	return logs, l
}

func toolLogContext(l *zap.Logger, traceID string) context.Context {
	return logger.WithLogger(context.Background(), l.With(zap.String("trace_id", traceID)))
}

func toolLogEntries(t *testing.T, logs *observer.ObservedLogs, level string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	for _, entry := range logs.All() {
		if entry.Message == "AI工具调用" && entry.Level.String() == level {
			entries = append(entries, entry.ContextMap())
		}
	}
	return entries
}

func assertCallIDsOnce(t *testing.T, entries []map[string]any, callIDs ...string) {
	t.Helper()
	counts := make(map[string]int, len(callIDs))
	for _, entry := range entries {
		if callID, ok := entry["tool_call_id"].(string); ok {
			counts[callID]++
		}
	}
	if len(entries) != len(callIDs) {
		t.Fatalf("调用日志条数=%d，期望=%d：%v", len(entries), len(callIDs), entries)
	}
	for _, callID := range callIDs {
		if counts[callID] != 1 {
			t.Errorf("call_id %q 日志数=%d，期望恰好 1 条", callID, counts[callID])
		}
	}
}

func assertNoToolPayload(t *testing.T, logs *observer.ObservedLogs) {
	t.Helper()
	for _, entry := range logs.All() {
		fields := fmt.Sprint(entry.ContextMap())
		if strings.Contains(entry.Message+fields, "private-argument") || strings.Contains(entry.Message+fields, "private-result") || strings.Contains(entry.Message+fields, "private-panic") {
			t.Fatalf("工具参数/结果正文进入日志：%s %s", entry.Message, fields)
		}
	}
}
