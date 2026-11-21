package sqlite

import (
	"context"
	"testing"

	"plumebot/internal/domain/entity"
)

func TestRecallLexicalScopeShortChineseAndVersionChecks(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	messages := []struct {
		id, session, group, user, kind, text string
	}{
		{"r1", "group-1", "group-1", "101", "group", "猫咪喜欢蓝色星球"},
		{"r2", "group-2", "group-2", "202", "group", "猫咪喜欢另一片星空"},
		{"r3", "private:101", "", "101", "private", "猫咪提到自己的私聊内容"},
		{"r4", "private:202", "", "202", "private", "猫咪提到另一位的私聊内容"},
	}
	for _, item := range messages {
		msg := entity.Message{
			MessageID: item.id, GroupID: item.group, UserID: item.user, MessageType: item.kind, Timestamp: 100,
			Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: item.text}},
		}
		if err := store.SaveMessage(ctx, item.session, msg); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := store.ListDirtyRecallSources(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if source.Kind != entity.RecallSourceMessage {
			continue
		}
		text := source.Message.ForLLM()
		if err := replaceTestRecallDocument(ctx, store, source, text, 100, 100); err != nil {
			t.Fatal(err)
		}
	}

	query := entity.RecallQuery{Text: "猫咪", Limit: 5}
	got, err := store.SearchRecallLexical(ctx, entity.RecallScope{SessionKeys: []string{"group-1"}}, query, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Document.SessionKey != "group-1" {
		t.Fatalf("短中文词或 scope 过滤错误: %+v", got)
	}
	for _, privateSession := range []string{"private:101", "private:202"} {
		privateGot, err := store.SearchRecallLexical(ctx, entity.RecallScope{SessionKeys: []string{privateSession}}, query, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(privateGot) != 1 || privateGot[0].Document.SessionKey != privateSession {
			t.Fatalf("私聊召回必须隔离会话 %q: %+v", privateSession, privateGot)
		}
	}
	longQuery := entity.RecallQuery{Text: "蓝色星球"}
	got, err = store.SearchRecallLexical(ctx, entity.RecallScope{SessionKeys: []string{"group-1"}}, longQuery, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Document.SourceID != sources[0].ID {
		t.Fatalf("FTS trigram 查询未命中: %+v sources=%+v", got, sources)
	}

	// 源版本递增后，旧派生文档必须立即从候选中隐藏，直到重新索引。
	if err := store.UpdateMessageParts(ctx, "r1", []entity.ContentPart{{Type: entity.PartTypeText, Text: "火星探险记录"}}); err != nil {
		t.Fatal(err)
	}
	got, err = store.SearchRecallLexical(ctx, entity.RecallScope{SessionKeys: []string{"group-1"}}, query, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("源更新后不应泄露旧索引内容: %+v", got)
	}
	coverage, err := store.RecallCoverage(ctx, entity.RecallScope{SessionKeys: []string{"group-1"}}, query, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !coverage.Indexing {
		t.Fatal("源变化待重新索引时应标记 Indexing")
	}
}

func TestRecallVectorCosineAndCoverage(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	msg := entity.Message{
		MessageID: "vector-message", GroupID: "g1", UserID: "7", MessageType: "group", Timestamp: 100,
		Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: "向量检索目标"}},
	}
	if err := store.SaveMessage(ctx, "g1", msg); err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListDirtyRecallSources(ctx, 10)
	if err != nil || len(sources) != 1 {
		t.Fatalf("读取待索引来源: %v, %+v", err, sources)
	}
	if err := replaceTestRecallDocument(ctx, store, sources[0], "向量检索目标", 100, 100); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListPendingRecallVectors(ctx, "model-v1", 0, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("读取待向量化文档: %v, %+v", err, pending)
	}
	coverage, err := store.RecallCoverage(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{}, "model-v1", 3)
	if err != nil || !coverage.Indexing {
		t.Fatalf("待向量化文档应标记 Indexing: %+v, err=%v", coverage, err)
	}
	if err := store.SaveRecallVectors(ctx, []entity.RecallVector{{
		DocumentID: pending[0].ID, ContentVersion: pending[0].Version, ModelKey: "model-v1", Values: []float32{1, 0, 0},
	}}); err != nil {
		t.Fatal(err)
	}
	coverage, err = store.RecallCoverage(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{}, "model-v1", 3)
	if err != nil || coverage.Indexing {
		t.Fatalf("向量写入后不应继续标记 Indexing: %+v, err=%v", coverage, err)
	}
	candidates, err := store.SearchRecallVectors(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{}, []float32{1, 0, 0}, "model-v1", 5)
	if err != nil {
		t.Fatalf("SQLite cosine 标量查询失败: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Document.SourceID != sources[0].ID {
		t.Fatalf("余弦向量检索未命中: %+v", candidates)
	}
	threshold := 0.9
	candidates, err = store.SearchRecallVectors(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{MinSimilarity: &threshold}, []float32{0, 1, 0}, "model-v1", 5)
	if err != nil {
		t.Fatalf("相似度阈值查询失败: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("低于阈值的候选不应返回: %+v", candidates)
	}
	// 维数不匹配时跳过候选而非让标量函数使整次查询失败。
	candidates, err = store.SearchRecallVectors(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{}, []float32{1, 0}, "model-v1", 5)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("维数不匹配应安全跳过: candidates=%+v err=%v", candidates, err)
	}
	coverage, err = store.RecallCoverage(ctx, entity.RecallScope{SessionKeys: []string{"g1"}}, entity.RecallQuery{}, "model-v1", 2)
	if err != nil || !coverage.Indexing {
		t.Fatalf("维数不匹配应标记待重建: coverage=%+v err=%v", coverage, err)
	}
	pending, err = store.ListPendingRecallVectors(ctx, "model-v1", 2, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("维数不匹配的文档应重新进入索引队列: pending=%+v err=%v", pending, err)
	}
}

func replaceTestRecallDocument(ctx context.Context, store *Storage, source entity.RecallSource, text string, from, to int64) error {
	doc := entity.RecallDocument{
		Kind: source.Kind, SourceID: source.ID, SegmentNo: 0, SessionKey: source.SessionKey,
		Version: source.Version, Text: text, ContentHash: "test-hash", EventFrom: &from, EventTo: &to,
		ToRune: len([]rune(text)),
	}
	_, err := store.ReplaceRecallDocuments(ctx, source, []entity.RecallDocument{doc})
	return err
}

func TestRecallUnknownSummaryTimeIsNotReplacedWithCreatedAt(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.SaveSummary(ctx, entity.Summary{ChatID: "g1", Seq: 1, Text: "未知时间摘要", CreatedAt: 9000}); err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListDirtyRecallSources(ctx, 10)
	if err != nil || len(sources) != 1 || sources[0].Summary == nil {
		t.Fatalf("读取旧摘要: sources=%+v err=%v", sources, err)
	}
	source := sources[0]
	if source.Summary.EventFrom != nil || source.Summary.EventTo != nil {
		t.Fatalf("旧摘要不得将 created_at 冒充事件时间: %+v", source.Summary)
	}
	doc := entity.RecallDocument{
		Kind: source.Kind, SourceID: source.ID, SessionKey: source.SessionKey, Version: source.Version,
		Text: source.Summary.Text, ContentHash: "unknown-time", SegmentNo: 0,
	}
	if _, err := store.ReplaceRecallDocuments(ctx, source, []entity.RecallDocument{doc}); err != nil {
		t.Fatal(err)
	}
	from, to := int64(100), int64(200)
	query := entity.RecallQuery{Text: "未知时间摘要", From: &from, To: &to}
	scope := entity.RecallScope{SessionKeys: []string{"g1"}}
	coverage, err := store.RecallCoverage(ctx, scope, query, "", 0)
	if err != nil || !coverage.UnknownTime {
		t.Fatalf("时间筛选应标记旧摘要时间未知: coverage=%+v err=%v", coverage, err)
	}
	got, err := store.SearchRecallLexical(ctx, scope, query, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("未知事件时间的摘要不得通过 created_at 命中时间范围: %+v", got)
	}
	query.From, query.To = nil, nil
	got, err = store.SearchRecallLexical(ctx, scope, query, 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("无时间筛选时旧摘要仍应可按关键词搜索: got=%+v err=%v", got, err)
	}
}

func TestRecallCoverageMarksOnlyCrossingTimeBoundaries(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	msg := entity.Message{
		MessageID: "old-message", GroupID: "g1", UserID: "7", MessageType: "group", Timestamp: 50,
		Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: "较早的对话"}},
	}
	if err := store.SaveMessage(ctx, "g1", msg); err != nil {
		t.Fatal(err)
	}
	sources, err := store.ListDirtyRecallSources(ctx, 10)
	if err != nil || len(sources) != 1 {
		t.Fatalf("读取消息来源: %v, %+v", err, sources)
	}
	if err := replaceTestRecallDocument(ctx, store, sources[0], "较早的对话", 50, 50); err != nil {
		t.Fatal(err)
	}
	from, to := int64(100), int64(200)
	query := entity.RecallQuery{From: &from, To: &to}
	scope := entity.RecallScope{SessionKeys: []string{"g1"}}
	coverage, err := store.RecallCoverage(ctx, scope, query, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.BoundaryExcluded {
		t.Fatal("完全落在筛选区间之前的文档不应被误判为跨界")
	}

	start, end := int64(90), int64(110)
	if err := store.SaveSummary(ctx, entity.Summary{ChatID: "g1", Seq: 1, Text: "覆盖边界的摘要", CreatedAt: 500, EventFrom: &start, EventTo: &end}); err != nil {
		t.Fatal(err)
	}
	sources, err = store.ListDirtyRecallSources(ctx, 10)
	if err != nil || len(sources) != 1 || sources[0].Kind != entity.RecallSourceSummary {
		t.Fatalf("读取摘要来源: %v, %+v", err, sources)
	}
	if err := replaceTestRecallDocument(ctx, store, sources[0], "覆盖边界的摘要", start, end); err != nil {
		t.Fatal(err)
	}
	coverage, err = store.RecallCoverage(ctx, scope, query, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !coverage.BoundaryExcluded {
		t.Fatal("跨越 from_time 的摘要应标记为边界排除")
	}
}
