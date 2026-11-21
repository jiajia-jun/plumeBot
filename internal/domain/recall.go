package domain

import (
	"context"

	"plumebot/internal/domain/entity"
)

// Embedder 将文本映射到同一向量空间。ModelKey 不含密钥，用于隔离模型版本。
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	ModelKey() string
	Dimensions() int // 首次成功 embedding 后返回向量维数；0 = 尚未知
}

// MemoryRecaller 提供 Agent 工具可用的历史记忆召回能力。
type MemoryRecaller interface {
	Recall(ctx context.Context, session entity.Session, query entity.RecallQuery) (entity.RecallResult, error)
}

// GroupMemberReader 绑定当前 ZeroBot 事件群，返回该群当前成员 QQ 号。
type GroupMemberReader interface {
	ListCurrentMembers(ctx context.Context) ([]string, error)
}

// RecallStore 是召回服务消费的窄检索接口。
type RecallStore interface {
	SearchRecallLexical(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, limit int) ([]entity.RecallCandidate, error)
	SearchRecallVectors(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, vector []float32, modelKey string, limit int) ([]entity.RecallCandidate, error)
	LoadRecallContext(ctx context.Context, scope entity.RecallScope, candidates []entity.RecallCandidate, query entity.RecallQuery) ([]entity.RecallCandidate, error)
	RecallCoverage(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, modelKey string, dimensions int) (entity.RecallCoverage, error)
}

// RecallIndexStore 是后台索引 worker 消费的窄存储接口。
type RecallIndexStore interface {
	ListDirtyRecallSources(ctx context.Context, limit int) ([]entity.RecallSource, error)
	ReplaceRecallDocuments(ctx context.Context, source entity.RecallSource, docs []entity.RecallDocument) (bool, error)
	ListPendingRecallVectors(ctx context.Context, modelKey string, dimensions int, limit int) ([]entity.RecallDocument, error)
	SaveRecallVectors(ctx context.Context, vectors []entity.RecallVector) error
}

type groupMemberReaderContextKey struct{}

// WithGroupMemberReader 注入当前事件的群成员查询能力。
func WithGroupMemberReader(ctx context.Context, reader GroupMemberReader) context.Context {
	return context.WithValue(ctx, groupMemberReaderContextKey{}, reader)
}

// GroupMemberReaderFrom 读取当前事件注入的群成员查询能力。
func GroupMemberReaderFrom(ctx context.Context) (GroupMemberReader, bool) {
	reader, ok := ctx.Value(groupMemberReaderContextKey{}).(GroupMemberReader)
	return reader, ok
}
