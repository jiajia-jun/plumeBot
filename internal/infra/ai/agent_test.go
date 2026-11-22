package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"

	"plumebot/internal/domain"
	"plumebot/internal/domain/entity"
	"plumebot/internal/infra/ai/toolmiddleware"
	recalltools "plumebot/internal/infra/ai/tools"
	"plumebot/internal/infra/sqlite"
	"plumebot/internal/service/memory"
	"plumebot/pkg/config"
)

// agentCfg 构造测试用 AgentConfig（name/description 固定，system_prompt 按用例传）。
func agentCfg(system string) config.AgentConfig {
	return config.AgentConfig{Name: "test-agent", Description: "测试 agent", SystemPrompt: system}
}

func sysMsg(text string) entity.ChatMessage {
	return entity.ChatMessage{Role: entity.RoleSystem, Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: text}}}
}

func usrMsg(text string) entity.ChatMessage {
	return entity.ChatMessage{Role: entity.RoleUser, Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: text}}}
}

// 文本往返：业务消息 → schema 消息 → 模型 → 最终文本；system+user 原样透传。
func TestEinoAgentTextRoundtrip(t *testing.T) {
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("你好，我是 PlumeBot。", nil),
	}}

	agent, err := NewEinoAgent(context.Background(), fake, nil, agentCfg(""))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}

	got, err := agent.Generate(context.Background(), []entity.ChatMessage{sysMsg("你是 PlumeBot。"), usrMsg("你好")})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if got != "你好，我是 PlumeBot。" {
		t.Errorf("回复 = %q，期望「你好，我是 PlumeBot。」", got)
	}

	in := fake.inputs[0]
	if len(in) != 2 {
		t.Fatalf("模型收到 %d 条消息，期望 2", len(in))
	}
	if in[0].Role != schema.System || in[1].Role != schema.User {
		t.Errorf("角色不符: %q / %q", in[0].Role, in[1].Role)
	}
	if in[0].Content != "你是 PlumeBot。" || in[1].Content != "你好" {
		t.Errorf("文本未透传: system=%q user=%q", in[0].Content, in[1].Content)
	}
}

// Instruction 注入：固定人设由 AgentConfig.SystemPrompt 提供，adk 在每次 Run 时前置为 system 消息
// （defaultGenModelInput），业务消息只传 user。
func TestEinoAgentInstructionInjected(t *testing.T) {
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("好的", nil),
	}}

	agent, err := NewEinoAgent(context.Background(), fake, nil, agentCfg("你是 PlumeBot，一个赛博群友。"))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}

	got, err := agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("你好")})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if got != "好的" {
		t.Errorf("回复 = %q", got)
	}

	in := fake.inputs[0]
	if len(in) != 2 {
		t.Fatalf("模型收到 %d 条消息，期望 2（instruction system + user）", len(in))
	}
	if in[0].Role != schema.System || in[0].Content != "你是 PlumeBot，一个赛博群友。" || in[1].Role != schema.User {
		t.Errorf("消息列表不符: %+v，期望 [system(instruction), user]", in)
	}
}

// name/description 透传：NewEinoAgent 把 AgentConfig 元数据写入 adk 配置（构造成功即被接受）。
func TestEinoAgentNameDescription(t *testing.T) {
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("ok", nil),
	}}
	agent, err := NewEinoAgent(context.Background(), fake, nil, agentCfg(""))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}
	if _, err := agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("hi")}); err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
}

// 模型错误（脚本用尽）应透传，adk 会包装错误信息。
func TestEinoAgentPropagatesModelError(t *testing.T) {
	fake := &fakeChatModel{} // 空脚本 → 首次调用即报错
	agent, err := NewEinoAgent(context.Background(), fake, nil, agentCfg(""))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}

	_, err = agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("hi")})
	if err == nil {
		t.Fatal("期望报错，实际无错误")
	}
	if !strings.Contains(err.Error(), "脚本用尽") {
		t.Errorf("错误应包含模型原始错误，实际 %q", err.Error())
	}
}

// echoArgs 是 echo 工具的参数结构（jsonschema 由 struct 推断）。
type echoArgs struct {
	Text string `json:"text" jsonschema:"description=要回显的文本"`
}

// tool 自动循环：模型先回 tool_call → echo 执行 → 结果回填 tool 消息 → 模型回最终答案。
func TestEinoAgentToolLoop(t *testing.T) {
	var echoCalled []string
	params, err := toolutils.GoStruct2ParamsOneOf[echoArgs]()
	if err != nil {
		t.Fatalf("GoStruct2ParamsOneOf 失败: %v", err)
	}
	echo := toolutils.NewTool(&schema.ToolInfo{Name: "echo", Desc: "回显文本", ParamsOneOf: params},
		func(_ context.Context, args echoArgs) (string, error) {
			echoCalled = append(echoCalled, args.Text)
			return args.Text, nil
		})

	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "call-1", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"hi"}`}},
		}),
		schema.AssistantMessage("已回显：hi", nil),
	}}

	agent, err := NewEinoAgent(context.Background(), fake, []tool.BaseTool{echo}, agentCfg(""))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}

	got, err := agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("echo hi")})
	if err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	if got != "已回显：hi" {
		t.Errorf("回复 = %q，期望「已回显：hi」", got)
	}
	if len(echoCalled) != 1 || echoCalled[0] != "hi" {
		t.Errorf("echo 执行记录 = %v，期望 [hi]", echoCalled)
	}

	// 第二轮输入应包含 tool 角色消息（工具结果回填）
	second := fake.inputs[1]
	foundToolMsg := false
	for _, m := range second {
		if m.Role == schema.Tool && m.ToolCallID == "call-1" && m.Content == "hi" {
			foundToolMsg = true
		}
	}
	if !foundToolMsg {
		t.Errorf("第二轮输入应含 tool 结果消息，实际 %+v", second)
	}
}

// TestEinoAgentToolOutputLimitMiddleware verifies the configured budget is propagated through Eino's tool loop.
func TestEinoAgentToolOutputLimitMiddleware(t *testing.T) {
	params, err := toolutils.GoStruct2ParamsOneOf[echoArgs]()
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	echo := toolutils.NewTool(&schema.ToolInfo{Name: "echo", Desc: "回显", ParamsOneOf: params},
		func(context.Context, echoArgs) (string, error) {
			called++
			return strings.Repeat("界", 100), nil
		})
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-limit", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"x"}`}}}),
		schema.AssistantMessage("完成", nil),
	}}
	agent, err := NewEinoAgent(context.Background(), fake, []tool.BaseTool{echo}, agentCfg(""), toolmiddleware.ToolOutputLimitConfig{PerCallChars: 32, TotalChars: 80})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("echo")}); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("正常预算下工具应执行一次，实际 %d", called)
	}
	var toolText string
	for _, message := range fake.inputs[1] {
		if message.Role == schema.Tool && message.ToolCallID == "call-limit" {
			toolText = message.Content
		}
	}
	if utf8.RuneCountInString(toolText) > 32 || !strings.Contains(toolText, "工具结果已截断") {
		t.Fatalf("Eino tool loop 未收到受限结果: %q", toolText)
	}
}

func TestEinoAgentBudgetNoticeSkipsSideEffectingTool(t *testing.T) {
	params, err := toolutils.GoStruct2ParamsOneOf[echoArgs]()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	echo := toolutils.NewTool(&schema.ToolInfo{Name: "echo", Desc: "副作用工具", ParamsOneOf: params},
		func(context.Context, echoArgs) (string, error) {
			called = true
			return "不应执行", nil
		})
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-budget", Function: schema.FunctionCall{Name: "echo", Arguments: `{"text":"x"}`}}}),
		schema.AssistantMessage("预算不足，未执行", nil),
	}}
	agent, err := NewEinoAgent(context.Background(), fake, []tool.BaseTool{echo}, agentCfg(""), toolmiddleware.ToolOutputLimitConfig{PerCallChars: 60, TotalChars: 40})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("echo")}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("仅剩预算提示空间时执行了可能有副作用的工具")
	}
	var toolText string
	for _, message := range fake.inputs[1] {
		if message.Role == schema.Tool && message.ToolCallID == "call-budget" {
			toolText = message.Content
		}
	}
	if !strings.Contains(toolText, "预算已耗尽") {
		t.Fatalf("模型收到的预算提示错误: %q", toolText)
	}
}

type recallMemberListStub struct {
	members []string
}

func (r recallMemberListStub) ListCurrentMembers(context.Context) ([]string, error) {
	return r.members, nil
}

func TestEinoRecallToolLoopUsesSQLiteAndCurrentMemberScope(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, message := range []struct {
		id, session, group, user, kind, text string
	}{
		{"group-hit", "g1", "g1", "101", "group", "周五见面"},
		{"member-private", "private:101", "", "101", "private", "约定下午三点碰面"},
		{"other-private", "private:202", "", "202", "private", "下午三点是秘密"},
	} {
		msg := entity.Message{
			MessageID: message.id, GroupID: message.group, UserID: message.user, MessageType: message.kind, Timestamp: 100,
			Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: message.text}},
		}
		if err := store.SaveMessage(ctx, message.session, msg); err != nil {
			t.Fatal(err)
		}
	}
	pending, err := store.ListDirtyRecallSources(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range pending {
		if source.Message == nil {
			continue
		}
		text := source.Message.ForLLM()
		if source.Message.MessageType == "group" && source.Message.UserID != "" {
			text = "[" + source.Message.UserID + "]: " + text
		}
		timestamp := source.Message.Timestamp
		doc := entity.RecallDocument{
			Kind: source.Kind, SourceID: source.ID, SegmentNo: 0, SessionKey: source.SessionKey,
			Version: source.Version, Text: text, ContentHash: "integration-test",
			FromRune: 0, ToRune: utf8.RuneCountInString(text), EventFrom: &timestamp, EventTo: &timestamp,
		}
		if applied, err := store.ReplaceRecallDocuments(ctx, source, []entity.RecallDocument{doc}); err != nil || !applied {
			t.Fatalf("建立测试召回索引: applied=%v err=%v", applied, err)
		}
	}

	recaller := memory.NewRecallService(store, nil)
	recallTool := recalltools.NewRecallTool(recaller).Tool()
	fake := &fakeChatModel{script: []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "recall-call", Function: schema.FunctionCall{Name: "recall_memory", Arguments: `{"query":"下午三点"}`},
		}}),
		schema.AssistantMessage("我找到以前的约定。", nil),
	}}
	agent, err := NewEinoAgent(ctx, fake, []tool.BaseTool{recallTool}, agentCfg(""), toolmiddleware.ToolOutputLimitConfig{PerCallChars: 1200, TotalChars: 2000})
	if err != nil {
		t.Fatal(err)
	}
	runCtx := entity.WithSession(ctx, entity.Session{GroupID: "g1", UserID: "101"})
	runCtx = domain.WithGroupMemberReader(runCtx, recallMemberListStub{members: []string{"101", "303"}})
	if _, err := agent.Generate(runCtx, []entity.ChatMessage{usrMsg("之前约了什么？")}); err != nil {
		t.Fatal(err)
	}
	var recallOutput string
	for _, message := range fake.inputs[1] {
		if message.Role == schema.Tool && message.ToolCallID == "recall-call" {
			recallOutput = message.Content
		}
	}
	if !strings.Contains(recallOutput, "约定下午三点碰面") || !strings.Contains(recallOutput, "QQ 101") {
		t.Fatalf("真实 Agent tool loop 未召回当前成员私聊内容: %q", recallOutput)
	}
	if strings.Contains(recallOutput, "下午三点是秘密") || strings.Contains(recallOutput, "QQ 202") {
		t.Fatalf("跨越成员范围泄露了其他用户私聊: %q", recallOutput)
	}
}

// 模型返回 nil 消息：adk 或 EinoAgent 应报错，不得返回空串静默成功。
func TestEinoAgentNilModelOutput(t *testing.T) {
	fake := &fakeChatModel{script: []*schema.Message{nil}}
	agent, err := NewEinoAgent(context.Background(), fake, nil, agentCfg(""))
	if err != nil {
		t.Fatalf("NewEinoAgent 失败: %v", err)
	}

	_, err = agent.Generate(context.Background(), []entity.ChatMessage{usrMsg("hi")})
	if err == nil {
		t.Fatal("模型返回 nil 消息应报错")
	}
	if !errors.Is(err, errNoModelOutput) && !strings.Contains(err.Error(), "nil") {
		t.Errorf("错误应来自 errNoModelOutput 或 adk nil 校验，实际 %q", err.Error())
	}
}
