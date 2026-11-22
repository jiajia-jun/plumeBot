package toolmiddleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"

	"plumebot/pkg/logger"
)

// NewToolCallLogger 创建工具调用终态日志中间件。
func NewToolCallLogger() compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				return withCallLog(ctx, input, func() (*compose.ToolOutput, error) {
					return next(ctx, input)
				})
			}
		},
		Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
				return withCallLog(ctx, input, func() (*compose.StreamToolOutput, error) {
					return next(ctx, input)
				})
			}
		},
		EnhancedInvokable: func(next compose.EnhancedInvokableToolEndpoint) compose.EnhancedInvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.EnhancedInvokableToolOutput, error) {
				return withCallLog(ctx, input, func() (*compose.EnhancedInvokableToolOutput, error) {
					return next(ctx, input)
				})
			}
		},
		EnhancedStreamable: func(next compose.EnhancedStreamableToolEndpoint) compose.EnhancedStreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.EnhancedStreamableToolOutput, error) {
				return withCallLog(ctx, input, func() (*compose.EnhancedStreamableToolOutput, error) {
					return next(ctx, input)
				})
			}
		},
	}
}

func withCallLog[T any](ctx context.Context, input *compose.ToolInput, next func() (T, error)) (output T, err error) {
	started := time.Now()
	defer func() {
		if recovered := recover(); recovered != nil {
			logToolCall(ctx, input.Name, input.CallID, started, "panic")
			panic(recovered)
		}

		outcome := "returned"
		if err != nil {
			outcome = "error"
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				outcome = "cancelled"
			}
		}
		logToolCall(ctx, input.Name, input.CallID, started, outcome)
	}()
	return next()
}

func logToolCall(ctx context.Context, name, callID string, started time.Time, outcome string) {
	fields := []zap.Field{
		logger.S("tool_name", name),
		logger.S("tool_call_id", callID),
		logger.S("outcome", outcome),
		logger.I64("latency_ms", time.Since(started).Milliseconds()),
	}

	l := logger.From(ctx)
	if outcome == "returned" {
		l.Info("AI工具调用", fields...)
		return
	}
	l.Warn("AI工具调用", fields...)
}

// HandleUnknownTool 记录模型请求的未知工具，并保持调用失败。
func HandleUnknownTool(ctx context.Context, name, _ string) (string, error) {
	started := time.Now()
	err := fmt.Errorf("tool %s not found in toolsNode indexes", name)
	logToolCall(ctx, name, compose.GetToolCallID(ctx), started, "unknown_tool")
	return "", err
}
