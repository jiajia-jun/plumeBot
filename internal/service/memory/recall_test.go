package memory

import (
	"context"
	"errors"
	"testing"

	"plumebot/internal/domain"
	"plumebot/internal/domain/entity"
)

type recallStoreStub struct {
	coverage      entity.RecallCoverage
	lexical       []entity.RecallCandidate
	vectors       []entity.RecallCandidate
	seenScope     entity.RecallScope
	loaded        []entity.RecallCandidate
	vectorQueried bool
}

func (s *recallStoreStub) SearchRecallLexical(_ context.Context, scope entity.RecallScope, _ entity.RecallQuery, _ int) ([]entity.RecallCandidate, error) {
	s.seenScope = scope
	return s.lexical, nil
}

func (s *recallStoreStub) SearchRecallVectors(_ context.Context, scope entity.RecallScope, _ entity.RecallQuery, _ []float32, _ string, _ int) ([]entity.RecallCandidate, error) {
	s.vectorQueried = true
	s.seenScope = scope
	return s.vectors, nil
}

func (s *recallStoreStub) LoadRecallContext(_ context.Context, _ entity.RecallScope, candidates []entity.RecallCandidate, _ entity.RecallQuery) ([]entity.RecallCandidate, error) {
	if s.loaded != nil {
		byID := make(map[int64]entity.RecallCandidate, len(s.loaded))
		for _, candidate := range s.loaded {
			byID[candidate.Document.ID] = candidate
		}
		out := make([]entity.RecallCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			if loaded, ok := byID[candidate.Document.ID]; ok {
				out = append(out, loaded)
			}
		}
		return out, nil
	}
	return candidates, nil
}

func (s *recallStoreStub) RecallCoverage(_ context.Context, _ entity.RecallScope, _ entity.RecallQuery, _ string, _ int) (entity.RecallCoverage, error) {
	return s.coverage, nil
}

type recallEmbedderStub struct {
	vector []float32
}

func (e recallEmbedderStub) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) != 1 {
		return nil, errors.New("expected one query")
	}
	return [][]float32{e.vector}, nil
}

func (e recallEmbedderStub) ModelKey() string { return "test-model" }
func (e recallEmbedderStub) Dimensions() int  { return len(e.vector) }

type groupMemberReaderStub struct {
	members []string
	err     error
}

func (r groupMemberReaderStub) ListCurrentMembers(context.Context) ([]string, error) {
	return r.members, r.err
}

func TestRecallServiceBuildsFailClosedScopes(t *testing.T) {
	store := &recallStoreStub{}
	svc := NewRecallService(store, nil)
	groupSession := entity.Session{GroupID: "g1", UserID: "100"}

	ctx := domain.WithGroupMemberReader(context.Background(), groupMemberReaderStub{members: []string{"200", "100", "bad", "0"}})
	result, err := svc.Recall(ctx, groupSession, entity.RecallQuery{Text: "旧约定"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"g1", "private:100", "private:200"}
	if len(store.seenScope.SessionKeys) != len(want) {
		t.Fatalf("群聊授权范围错误: %+v", store.seenScope.SessionKeys)
	}
	for i := range want {
		if store.seenScope.SessionKeys[i] != want[i] {
			t.Fatalf("群聊授权范围 = %+v, want %+v", store.seenScope.SessionKeys, want)
		}
	}
	if !result.Partial {
		t.Fatal("成员响应含非法 ID 时应标记 partial")
	}

	store.seenScope = entity.RecallScope{}
	ctx = domain.WithGroupMemberReader(context.Background(), groupMemberReaderStub{err: errors.New("offline")})
	result, err = svc.Recall(ctx, groupSession, entity.RecallQuery{Text: "旧约定"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.seenScope.SessionKeys) != 1 || store.seenScope.SessionKeys[0] != "g1" || !result.Partial {
		t.Fatalf("成员列表失败必须 fail-closed 到本群并标记 partial: scope=%+v result=%+v", store.seenScope, result)
	}

	store.seenScope = entity.RecallScope{}
	_, err = svc.Recall(context.Background(), entity.Session{UserID: "300"}, entity.RecallQuery{Text: "私聊"})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.seenScope.SessionKeys) != 1 || store.seenScope.SessionKeys[0] != "private:300" {
		t.Fatalf("私聊范围必须只包含当前用户: %+v", store.seenScope.SessionKeys)
	}
}

func TestRecallServiceFusesAndDeduplicatesContexts(t *testing.T) {
	doc1 := entity.RecallDocument{ID: 1, Kind: entity.RecallSourceMessage, SessionKey: "g1", Text: "第一段"}
	doc2 := entity.RecallDocument{ID: 2, Kind: entity.RecallSourceMessage, SessionKey: "g1", Text: "更早的约定"}
	store := &recallStoreStub{
		lexical: []entity.RecallCandidate{
			{Document: doc1, Rank: 1},
			{Document: doc2, Rank: 2},
		},
		vectors: []entity.RecallCandidate{{Document: doc2, Rank: 1}},
		loaded: []entity.RecallCandidate{
			{Document: doc1, Messages: []entity.RecallMessage{{ID: 10, Text: "甲"}, {ID: 11, Text: "乙"}}},
			{Document: doc2, Messages: []entity.RecallMessage{{ID: 11, Text: "乙"}, {ID: 12, Text: "丙"}}},
		},
	}
	svc := NewRecallService(store, recallEmbedderStub{vector: []float32{1, 0}})
	result, err := svc.Recall(context.Background(), entity.Session{GroupID: "g1"}, entity.RecallQuery{Text: "约定", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if !store.vectorQueried {
		t.Fatal("启用 embedder 后应执行向量召回")
	}
	if len(result.Items) != 2 {
		t.Fatalf("融合结果条数错误: %+v", result.Items)
	}
	if len(result.Items[0].Messages) != 2 || result.Items[0].Messages[0].ID != 11 || result.Items[0].Messages[1].ID != 12 {
		t.Fatalf("RRF 排名或重复消息合并错误: %+v", result.Items)
	}
	if len(result.Items[1].Messages) != 1 || result.Items[1].Messages[0].ID != 10 {
		t.Fatalf("重叠上下文未去重: %+v", result.Items)
	}
}

func TestBuildRecallDocumentsKeepsUnknownMessageTimeUnknown(t *testing.T) {
	source := entity.RecallSource{
		Kind: entity.RecallSourceMessage, ID: 8, SessionKey: "private:9", Version: 1,
		Message: &entity.Message{MessageID: "m", UserID: "9", Timestamp: 0, MessageType: "private",
			Parts: []entity.ContentPart{{Type: entity.PartTypeText, Text: "历史文本"}}},
	}
	docs := buildRecallDocuments(source)
	if len(docs) != 1 || docs[0].EventFrom != nil || docs[0].EventTo != nil {
		t.Fatalf("时间戳无效的消息不能伪装为 epoch 事件: %+v", docs)
	}
}
