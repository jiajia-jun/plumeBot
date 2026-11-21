package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultToolOutputChars      = 2500
	defaultToolTotalOutputChars = 6000
	hardToolOutputChars         = 4000
	budgetMessage               = "本轮工具输出预算已耗尽，无法执行更多工具调用。"
	truncatedMessage            = "…[工具结果已截断]"
)

// ToolOutputLimitConfig 配置 Agent 每轮工具输出的字符预算。
type ToolOutputLimitConfig struct {
	PerCallChars int
	TotalChars   int
	MaxChars     int
}

type toolOutputBudgetContextKey struct{}

type toolOutputBudget struct {
	mu        sync.Mutex
	remaining int
}

func withToolOutputBudget(ctx context.Context, cfg ToolOutputLimitConfig) context.Context {
	if cfg.TotalChars <= 0 {
		cfg.TotalChars = defaultToolTotalOutputChars
	}
	return context.WithValue(ctx, toolOutputBudgetContextKey{}, &toolOutputBudget{remaining: cfg.TotalChars})
}

func budgetFromContext(ctx context.Context) *toolOutputBudget {
	budget, _ := ctx.Value(toolOutputBudgetContextKey{}).(*toolOutputBudget)
	return budget
}

func (b *toolOutputBudget) reserve(requested int) (reserved int, proceed bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining >= requested {
		b.remaining -= requested
		return requested, true
	}
	// Keep enough of the remaining budget to tell the model why the call was skipped.
	messageChars := utf8.RuneCountInString(budgetMessage)
	if b.remaining >= messageChars {
		b.remaining -= messageChars
		return messageChars, false
	}
	return 0, false
}

func (b *toolOutputBudget) refund(reserved, used int) {
	if b == nil || used >= reserved {
		return
	}
	b.mu.Lock()
	b.remaining += reserved - used
	b.mu.Unlock()
}

// NewToolOutputLimiter 建立 Eino 洋葱式工具中间件，覆盖普通、流式及增强工具。
func NewToolOutputLimiter(cfg ToolOutputLimitConfig) compose.ToolMiddleware {
	if cfg.PerCallChars <= 0 {
		cfg.PerCallChars = defaultToolOutputChars
	}
	if cfg.TotalChars <= 0 {
		cfg.TotalChars = defaultToolTotalOutputChars
	}
	if cfg.MaxChars <= 0 || cfg.MaxChars > hardToolOutputChars {
		cfg.MaxChars = hardToolOutputChars
	}
	perCall := cfg.PerCallChars
	if perCall > cfg.MaxChars {
		perCall = cfg.MaxChars
	}
	reserveFor := func(ctx context.Context, args string) (*toolOutputBudget, int, int, bool, error) {
		limit := perCall
		if requested := maxCharsFromArguments(args); requested > 0 && requested < limit {
			limit = requested
		}
		if limit > cfg.MaxChars {
			limit = cfg.MaxChars
		}
		budget := budgetFromContext(ctx)
		if budget == nil {
			return nil, limit, 0, false, nil
		}
		reserved, proceed := budget.reserve(limit)
		if reserved == 0 {
			return budget, 0, 0, false, errors.New(budgetMessage)
		}
		if !proceed {
			return budget, reserved, reserved, true, nil
		}
		return budget, limit, reserved, false, nil
	}

	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				budget, limit, reserved, skip, err := reserveFor(ctx, input.Arguments)
				if err != nil {
					return nil, err
				}
				if skip {
					return &compose.ToolOutput{Result: budgetMessage}, nil
				}
				output, err := next(ctx, input)
				if err != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, err
				}
				if output == nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return &compose.ToolOutput{}, nil
				}
				output.Result, _ = clampToolOutput(output.Result, limit)
				if budget != nil {
					budget.refund(reserved, utf8.RuneCountInString(output.Result))
				}
				return output, nil
			}
		},
		Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
				budget, limit, reserved, skip, err := reserveFor(ctx, input.Arguments)
				if err != nil {
					return nil, err
				}
				if skip {
					return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{budgetMessage})}, nil
				}
				output, err := next(ctx, input)
				if err != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, err
				}
				if output == nil || output.Result == nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{""})}, nil
				}
				text, truncated, readErr := collectTextStream(output.Result, limit)
				if readErr != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, readErr
				}
				if truncated {
					text = truncateStreamOutput(text, limit)
				}
				if budget != nil {
					budget.refund(reserved, utf8.RuneCountInString(text))
				}
				return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{text})}, nil
			}
		},
		EnhancedInvokable: func(next compose.EnhancedInvokableToolEndpoint) compose.EnhancedInvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.EnhancedInvokableToolOutput, error) {
				budget, limit, reserved, skip, err := reserveFor(ctx, input.Arguments)
				if err != nil {
					return nil, err
				}
				if skip {
					return &compose.EnhancedInvokableToolOutput{Result: textToolResult(budgetMessage)}, nil
				}
				output, err := next(ctx, input)
				if err != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, err
				}
				if output == nil || output.Result == nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return output, nil
				}
				used, tooLarge := limitToolResult(output.Result, limit)
				if tooLarge {
					output.Result = textToolResult(truncatedMessage)
					used = utf8.RuneCountInString(truncatedMessage)
				}
				if budget != nil {
					budget.refund(reserved, used)
				}
				return output, nil
			}
		},
		EnhancedStreamable: func(next compose.EnhancedStreamableToolEndpoint) compose.EnhancedStreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.EnhancedStreamableToolOutput, error) {
				budget, limit, reserved, skip, err := reserveFor(ctx, input.Arguments)
				if err != nil {
					return nil, err
				}
				if skip {
					return &compose.EnhancedStreamableToolOutput{Result: schema.StreamReaderFromArray([]*schema.ToolResult{textToolResult(budgetMessage)})}, nil
				}
				output, err := next(ctx, input)
				if err != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, err
				}
				if output == nil || output.Result == nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return &compose.EnhancedStreamableToolOutput{Result: schema.StreamReaderFromArray([]*schema.ToolResult{textToolResult("")})}, nil
				}
				result, used, tooLarge, readErr := collectEnhancedStream(output.Result, limit)
				if readErr != nil {
					if budget != nil {
						budget.refund(reserved, 0)
					}
					return nil, readErr
				}
				if tooLarge {
					result = textToolResult(truncatedMessage)
					used = utf8.RuneCountInString(truncatedMessage)
				}
				if budget != nil {
					budget.refund(reserved, used)
				}
				return &compose.EnhancedStreamableToolOutput{Result: schema.StreamReaderFromArray([]*schema.ToolResult{result})}, nil
			}
		},
	}
}

func maxCharsFromArguments(arguments string) int {
	var args struct {
		MaxChars int `json:"max_chars"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return 0
	}
	return args.MaxChars
}

func clampToolOutput(text string, limit int) (string, bool) {
	if limit <= 0 {
		return "", true
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text, false
	}
	if json.Valid([]byte(text)) {
		// Truncated JSON stays valid and carries an explicit truncation flag.
		return jsonTruncationEnvelope(string(runes), limit), true
	}
	return truncatePlainText(string(runes), limit), true
}

func jsonTruncationEnvelope(preview string, limit int) string {
	type envelope struct {
		Truncated bool   `json:"truncated"`
		Preview   string `json:"preview,omitempty"`
	}
	makeEnvelope := func(value string) string {
		encoded, _ := json.Marshal(envelope{Truncated: true, Preview: value})
		return string(encoded)
	}
	if limit <= 0 {
		return ""
	}
	if len(makeEnvelope("")) > limit {
		if limit >= 2 {
			return "{}"
		}
		return "0"
	}
	runes := []rune(preview)
	low, high := 0, len(runes)
	best := makeEnvelope("")
	for low <= high {
		mid := low + (high-low)/2
		candidate := makeEnvelope(string(runes[:mid]))
		if utf8.RuneCountInString(candidate) <= limit {
			best = candidate
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best
}

func truncatePlainText(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	marker := []rune(truncatedMessage)
	if len(marker) >= limit {
		return string(marker[:limit])
	}
	keep := limit - len(marker)
	if keep > len(runes) {
		keep = len(runes)
	}
	return string(runes[:keep]) + truncatedMessage
}

func truncateStreamOutput(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") ||
		strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "true") ||
		strings.HasPrefix(trimmed, "false") || strings.HasPrefix(trimmed, "null") {
		return jsonTruncationEnvelope(text, limit)
	}
	return truncatePlainText(text, limit)
}

func collectTextStream(reader *schema.StreamReader[string], limit int) (string, bool, error) {
	defer reader.Close()
	var b strings.Builder
	used := 0
	truncated := false
	for {
		chunk, err := reader.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false, err
		}
		remaining := limit + 1 - used
		if remaining <= 0 {
			truncated = true
			break
		}
		runes := []rune(chunk)
		if len(runes) > remaining {
			runes = runes[:remaining]
			truncated = true
		}
		b.WriteString(string(runes))
		used += len(runes)
		if used > limit {
			truncated = true
		}
		if truncated {
			break
		}
	}
	return b.String(), truncated, nil
}

func limitToolResult(result *schema.ToolResult, limit int) (int, bool) {
	used := 0
	for _, part := range result.Parts {
		if part.Type == schema.ToolPartTypeText {
			used += utf8.RuneCountInString(part.Text)
			if len(part.Extra) > 0 {
				if extra, err := json.Marshal(part.Extra); err == nil {
					used += len(extra)
				}
			}
		} else if serialized, err := json.Marshal(part); err == nil {
			// Non-text payloads (including base64 data and metadata) are indivisible;
			// count their serialized size and replace the whole result if it exceeds the cap.
			used += len(serialized)
		}
		if used > limit {
			return used, true
		}
	}
	return used, false
}

func textToolResult(text string) *schema.ToolResult {
	return &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: text}}}
}

func collectEnhancedStream(reader *schema.StreamReader[*schema.ToolResult], limit int) (*schema.ToolResult, int, bool, error) {
	defer reader.Close()
	result := &schema.ToolResult{}
	used := 0
	for {
		chunk, err := reader.Recv()
		if errors.Is(err, io.EOF) {
			return result, used, false, nil
		}
		if err != nil {
			return nil, 0, false, err
		}
		if chunk == nil {
			continue
		}
		chunkUsed, tooLarge := limitToolResult(chunk, limit-used)
		if tooLarge {
			return result, used + chunkUsed, true, nil
		}
		used += chunkUsed
		result.Parts = append(result.Parts, chunk.Parts...)
	}
}
