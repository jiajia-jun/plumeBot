package toolmiddleware

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func TestClampToolOutputUnicodeAndJSON(t *testing.T) {
	plain, truncated := clampToolOutput(strings.Repeat("界🙂", 30), 40)
	if !truncated {
		t.Fatal("超长纯文本应标记截断")
	}
	if got := utf8.RuneCountInString(plain); got > 40 {
		t.Fatalf("纯文本长度 = %d，超过 40", got)
	}
	if !strings.Contains(plain, truncatedMessage) {
		t.Fatalf("缺少截断标记: %q", plain)
	}

	input := `{"text":"` + strings.Repeat("界", 100) + `"}`
	got, truncated := clampToolOutput(input, 64)
	if !truncated || !json.Valid([]byte(got)) {
		t.Fatalf("JSON 截断后必须仍合法且有截断标记: truncated=%v result=%q", truncated, got)
	}
	var envelope struct {
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil || !envelope.Truncated {
		t.Fatalf("JSON 截断包络缺少标记: %q, err=%v", got, err)
	}
	if utf8.RuneCountInString(got) > 64 {
		t.Fatalf("JSON 包络长度 = %d，超过 64", utf8.RuneCountInString(got))
	}
}

func TestToolOutputLimiterKeepsShortAndClampsLong(t *testing.T) {
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 32, TotalChars: 80})
	endpoint := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return &compose.ToolOutput{Result: strings.Repeat("字", 100)}, nil
	})
	ctx := WithToolOutputBudget(context.Background(), ToolOutputLimitConfig{TotalChars: 80})
	out, err := endpoint(ctx, &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if got := utf8.RuneCountInString(out.Result); got > 32 {
		t.Fatalf("结果长度 = %d，超过单次限制 32", got)
	}
	if !strings.Contains(out.Result, truncatedMessage) {
		t.Fatalf("结果缺少截断标记: %q", out.Result)
	}

	short := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return &compose.ToolOutput{Result: "保留短结果"}, nil
	})
	shortCtx := WithToolOutputBudget(context.Background(), ToolOutputLimitConfig{TotalChars: 80})
	shortOut, err := short(shortCtx, &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if shortOut.Result != "保留短结果" {
		t.Fatalf("短结果被改写: %q", shortOut.Result)
	}
	if got := budgetFromContext(shortCtx).remaining; got != 80-utf8.RuneCountInString("保留短结果") {
		t.Fatalf("预算未按实际输出退还，剩余=%d", got)
	}
}

func TestToolOutputLimiterSkipsCallWhenOnlyBudgetNoticeFits(t *testing.T) {
	called := false
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 40})
	endpoint := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		called = true
		return &compose.ToolOutput{Result: "不应执行"}, nil
	})
	ctx := WithToolOutputBudget(context.Background(), ToolOutputLimitConfig{TotalChars: 40})
	out, err := endpoint(ctx, &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("剩余预算不足以预留单次上限时仍执行了工具")
	}
	if out.Result != budgetMessage {
		t.Fatalf("预算提示 = %q", out.Result)
	}
	if got := budgetFromContext(ctx).remaining; got != 40-utf8.RuneCountInString(budgetMessage) {
		t.Fatalf("预算提示未计入本轮预算，剩余=%d", got)
	}
}

func TestToolOutputLimiterParallelBudgetIsAtomic(t *testing.T) {
	var calls int
	var callsMu sync.Mutex
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 100})
	endpoint := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		callsMu.Lock()
		calls++
		callsMu.Unlock()
		started <- struct{}{}
		<-release
		return &compose.ToolOutput{Result: "短回执"}, nil
	})
	ctx := WithToolOutputBudget(context.Background(), ToolOutputLimitConfig{TotalChars: 100})
	firstOutput := make(chan string, 1)
	go func() {
		out, err := endpoint(ctx, &compose.ToolInput{Arguments: `{}`})
		if err != nil {
			firstOutput <- "ERROR:" + err.Error()
			return
		}
		firstOutput <- out.Result
	}()
	<-started // first call has reserved its maximum while still executing
	second, err := endpoint(ctx, &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	first := <-firstOutput
	if second.Result != budgetMessage || first != "短回执" {
		t.Fatalf("预算结果不符：first=%q second=%q", first, second.Result)
	}
	if calls != 1 {
		t.Fatalf("预算不足时仍执行了 %d 次工具", calls)
	}
}

func TestToolOutputLimiterStreamAndEnhanced(t *testing.T) {
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 32, TotalChars: 100})
	stream := middleware.Streamable(func(context.Context, *compose.ToolInput) (*compose.StreamToolOutput, error) {
		return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{strings.Repeat("流", 80)})}, nil
	})
	streamOut, err := stream(context.Background(), &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	streamText, err := streamOut.Result.Recv()
	if err != nil {
		t.Fatal(err)
	}
	streamOut.Result.Close()
	if utf8.RuneCountInString(streamText) > 32 || !strings.Contains(streamText, truncatedMessage) {
		t.Fatalf("流式输出未限长: %q", streamText)
	}

	payload := strings.Repeat("A", 100)
	enhanced := middleware.EnhancedInvokable(func(context.Context, *compose.ToolInput) (*compose.EnhancedInvokableToolOutput, error) {
		return &compose.EnhancedInvokableToolOutput{Result: &schema.ToolResult{Parts: []schema.ToolOutputPart{{
			Type:  schema.ToolPartTypeImage,
			Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{Base64Data: &payload}},
		}}}}, nil
	})
	enhancedOut, err := enhanced(context.Background(), &compose.ToolInput{Arguments: `{}`})
	if len(enhancedOut.Result.Parts) != 1 || enhancedOut.Result.Parts[0].Type != schema.ToolPartTypeText || enhancedOut.Result.Parts[0].Text != truncatedMessage {
		t.Fatalf("超限的增强输出应整体替换为文本提示: %+v", enhancedOut.Result)
	}
}

func TestToolOutputLimiterJSONStreamAndRequestedLimit(t *testing.T) {
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 80, TotalChars: 200})
	stream := middleware.Streamable(func(context.Context, *compose.ToolInput) (*compose.StreamToolOutput, error) {
		jsonText := `{"message":"` + strings.Repeat("x", 100) + `"}`
		return &compose.StreamToolOutput{Result: schema.StreamReaderFromArray([]string{jsonText})}, nil
	})
	output, err := stream(context.Background(), &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := output.Result.Recv()
	if err != nil {
		t.Fatal(err)
	}
	output.Result.Close()
	if !json.Valid([]byte(got)) || utf8.RuneCountInString(got) > 80 {
		t.Fatalf("截断后的 JSON stream 无效或超长: %q", got)
	}
	var envelope struct {
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(got), &envelope); err != nil || !envelope.Truncated {
		t.Fatalf("JSON stream 截断包络缺少标记: %q err=%v", got, err)
	}

	limited := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return &compose.ToolOutput{Result: strings.Repeat("字", 100)}, nil
	})
	outputText, err := limited(context.Background(), &compose.ToolInput{Arguments: `{"max_chars":20}`})
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(outputText.Result) > 20 {
		t.Fatalf("工具输入 max_chars 未降低单次上限: %q", outputText.Result)
	}
}

func TestToolOutputLimiterHardCapAndNoRoomError(t *testing.T) {
	hardCap := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 9000, TotalChars: 9000})
	endpoint := hardCap.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		return &compose.ToolOutput{Result: strings.Repeat("长", 5000)}, nil
	})
	out, err := endpoint(context.Background(), &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	if got := utf8.RuneCountInString(out.Result); got > hardToolOutputChars {
		t.Fatalf("硬上限未生效: len=%d", got)
	}

	called := false
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 40, TotalChars: 10})
	blocked := middleware.Invokable(func(context.Context, *compose.ToolInput) (*compose.ToolOutput, error) {
		called = true
		return &compose.ToolOutput{Result: "副作用"}, nil
	})
	ctx := WithToolOutputBudget(context.Background(), ToolOutputLimitConfig{TotalChars: 10})
	if _, err := blocked(ctx, &compose.ToolInput{Arguments: `{}`}); err == nil {
		t.Fatal("剩余预算连提示都放不下时应返回错误")
	}
	if called {
		t.Fatal("无预算容纳提示时不得执行工具")
	}
}

func TestToolOutputLimiterEnhancedStream(t *testing.T) {
	middleware := NewToolOutputLimiter(ToolOutputLimitConfig{PerCallChars: 40, TotalChars: 100})
	endpoint := middleware.EnhancedStreamable(func(context.Context, *compose.ToolInput) (*compose.EnhancedStreamableToolOutput, error) {
		result := &schema.ToolResult{Parts: []schema.ToolOutputPart{{Type: schema.ToolPartTypeText, Text: strings.Repeat("增强", 100)}}}
		return &compose.EnhancedStreamableToolOutput{Result: schema.StreamReaderFromArray([]*schema.ToolResult{result})}, nil
	})
	out, err := endpoint(context.Background(), &compose.ToolInput{Arguments: `{}`})
	if err != nil {
		t.Fatal(err)
	}
	result, err := out.Result.Recv()
	if err != nil {
		t.Fatal(err)
	}
	out.Result.Close()
	if len(result.Parts) != 1 || result.Parts[0].Type != schema.ToolPartTypeText || result.Parts[0].Text != truncatedMessage {
		t.Fatalf("增强流式输出超限时应整体替换: %+v", result)
	}
}
