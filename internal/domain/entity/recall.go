package entity

// RecallQuery 是历史召回的归一化查询。From/To 为 Unix 秒，范围左闭右开。
type RecallQuery struct {
	Text          string
	From          *int64
	To            *int64
	Limit         int
	MaxMessages   int
	Candidates    int
	MinSimilarity *float64
}

// RecallScope 是服务端按当前会话和权限算出的可检索会话集合，不接收模型直接提供的范围。
type RecallScope struct {
	SessionKeys []string
}

const (
	RecallSourceMessage = "message"
	RecallSourceSummary = "summary"
)

// RecallSource 是待构建索引的原始消息或归档摘要。
type RecallSource struct {
	Kind       string
	ID         int64 // SQLite rowid；不用可能重复/空的 OneBot message_id
	SessionKey string
	Version    int64
	Message    *Message
	Summary    *Summary
}

// RecallDocument 是可独立做词法/向量检索的有界内容片段。
type RecallDocument struct {
	ID          int64
	Kind        string
	SourceID    int64
	SegmentNo   int
	SessionKey  string
	Version     int64
	Text        string
	ContentHash string
	FromRune    int
	ToRune      int
	EventFrom   *int64
	EventTo     *int64
}

// RecallMessage 是召回结果中可供模型引用的消息视图。
type RecallMessage struct {
	ID        int64
	Timestamp int64
	UserID    string
	Text      string
}

// RecallCandidate 是单个词法/向量分支返回的候选；Rank 越小越相关。
type RecallCandidate struct {
	Document RecallDocument
	Rank     int
	Score    float64
	Messages []RecallMessage // 原文候选的邻接上下文；摘要候选为空
}

// RecallCoverage 描述索引或精确时间范围尚未覆盖的部分。
type RecallCoverage struct {
	Indexing         bool
	UnknownTime      bool
	BoundaryExcluded bool
}

// RecallItem 是已合并排序后的可展示历史内容，不暴露内部来源类型。
type RecallItem struct {
	Messages  []RecallMessage
	Text      string
	EventFrom *int64
	EventTo   *int64
}

// RecallResult 是工具可展示的召回结果。
type RecallResult struct {
	Items   []RecallItem
	Partial bool
	Reasons []string
}

// RecallVector 是一个文档在特定模型空间中的向量。
type RecallVector struct {
	DocumentID     int64
	ContentVersion int64
	ModelKey       string
	Values         []float32
}
