package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"

	"plumebot/internal/domain/entity"
)

type recallStub struct {
	session entity.Session
	query   entity.RecallQuery
	result  entity.RecallResult
	err     error
	called  bool
}

func (s *recallStub) Recall(_ context.Context, session entity.Session, query entity.RecallQuery) (entity.RecallResult, error) {
	s.called = true
	s.session = session
	s.query = query
	return s.result, s.err
}

func TestRecallToolValidatesAndBoundsInput(t *testing.T) {
	stub := &recallStub{result: entity.RecallResult{}}
	tool := NewRecallTool(stub)
	ctx := entity.WithSession(context.Background(), entity.Session{GroupID: "g1", UserID: "123"})
	minSimilarity := 0.4
	got, err := tool.run(ctx, recallArgs{
		Query: "  上次说的计划  ", FromTime: "2026-06-01T00:00:00+08:00", ToTime: "2026-06-02T00:00:00+08:00",
		Limit: 99, MaxMessages: 99, Candidates: 999, MinSimilarity: &minSimilarity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !stub.called {
		t.Fatal("合法输入没有触发召回")
	}
	if stub.session.GroupID != "g1" || stub.query.Text != "上次说的计划" || stub.query.Limit != recallToolMaxLimit ||
		stub.query.MaxMessages != recallToolMaxMessages || stub.query.Candidates != recallToolMaxCandidates ||
		stub.query.MinSimilarity == nil || *stub.query.MinSimilarity != minSimilarity {
		t.Fatalf("参数未被安全归一: session=%+v query=%+v", stub.session, stub.query)
	}
	if got == "" {
		t.Fatal("空结果应给出清晰状态文本")
	}
}

func TestRecallToolFailsClosedAndRejectsInvalidTime(t *testing.T) {
	stub := &recallStub{}
	tool := NewRecallTool(stub)
	got, err := tool.run(context.Background(), recallArgs{Query: "找一下"})
	if err != nil || !strings.Contains(got, "缺少当前会话身份") || stub.called {
		t.Fatalf("无 session 时必须 fail-closed: got=%q err=%v called=%v", got, err, stub.called)
	}
	ctx := entity.WithSession(context.Background(), entity.Session{UserID: "123"})
	got, err = tool.run(ctx, recallArgs{Query: "找一下", FromTime: "2026/06/01"})
	if err != nil || !strings.Contains(got, "RFC3339") || stub.called {
		t.Fatalf("无效时间必须在检索前拒绝: got=%q err=%v called=%v", got, err, stub.called)
	}
}

func TestRecallToolFormatsUntrustedHistoryAndRecoversErrors(t *testing.T) {
	from, to := int64(100), int64(200)
	stub := &recallStub{result: entity.RecallResult{
		Partial: true, Reasons: []string{"部分历史仍在索引"},
		Items: []entity.RecallItem{{EventFrom: &from, EventTo: &to, Messages: []entity.RecallMessage{{
			Timestamp: 150, UserID: "123", Text: "历史内容（仅供参考）",
		}}}},
	}}
	tool := NewRecallTool(stub)
	ctx := entity.WithSession(context.Background(), entity.Session{UserID: "123"})
	got, err := tool.run(ctx, recallArgs{Query: "以前说了什么"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"部分历史", "1970-01-01T00:01:40Z", "QQ 123", "历史内容（仅供参考）"} {
		if !strings.Contains(got, part) {
			t.Fatalf("召回输出缺少 %q: %s", part, got)
		}
	}

	stub.err = errors.New("暂时不可用")
	got, err = tool.run(ctx, recallArgs{Query: "以前说了什么"})
	if err != nil || !strings.Contains(got, "历史召回暂不可用") {
		t.Fatalf("可恢复检索错误应转为文本结果: got=%q err=%v", got, err)
	}
}

func TestRecallToolExposesToolMetadata(t *testing.T) {
	base := NewRecallTool(nil).Tool()
	info, err := base.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "recall_memory" || info.ParamsOneOf == nil {
		t.Fatalf("tool schema 未构造: %+v", info)
	}
	if _, ok := base.(tool.InvokableTool); !ok {
		t.Fatalf("RecallTool 未实现 Eino InvokableTool 接口: %T", base)
	}
	if _, ok := base.(tool.StreamableTool); ok {
		t.Fatal("普通 recall 工具不应意外成为 StreamableTool")
	}
}

func TestRecallToolRejectsNegativeLimitsAndRoundsFractionalSeconds(t *testing.T) {
	stub := &recallStub{}
	tool := NewRecallTool(stub)
	ctx := entity.WithSession(context.Background(), entity.Session{UserID: "123"})
	got, err := tool.run(ctx, recallArgs{Query: "检索", Limit: -1})
	if err != nil || !strings.Contains(got, "不得为负数") || stub.called {
		t.Fatalf("负数限制必须在检索前拒绝: got=%q err=%v called=%v", got, err, stub.called)
	}
	parsed, err := parseRecallTime("1970-01-01T00:00:00.5Z")
	if err != nil || parsed == nil || *parsed != 1 {
		t.Fatalf("秒精度存储的分数时间边界应向上取整: parsed=%v err=%v", parsed, err)
	}
}
