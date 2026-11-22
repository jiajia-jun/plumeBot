package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"plumebot/internal/domain"
	"plumebot/internal/domain/entity"
	"plumebot/pkg/logger"
)

const (
	recallDefaultLimit       = 5
	recallMaxLimit           = 10
	recallDefaultMaxMessages = 8
	recallHardMaxMessages    = 12
	recallDefaultCandidates  = 30
	recallMaxCandidates      = 100
	recallMaxQueryRunes      = 256
	recallMaxScopeMembers    = 3000
	recallDocumentRunes      = 1500
	recallIndexBatch         = 32
	recallEmbedBatch         = 24
	recallIndexInterval      = 3 * time.Second
	recallIndexBatchInterval = 1 * time.Second
	recallIndexMaxBackoff    = 1 * time.Minute
	recallCallTimeout        = 8 * time.Second
	recallServiceTimeout     = 20 * time.Second
	rrfConstant              = 60.0
)

// RecallService 负责计算授权范围、调用混合检索并完成排名融合/去重。
type RecallService struct {
	store    domain.RecallStore
	embedder domain.Embedder
}

func NewRecallService(store domain.RecallStore, embedder domain.Embedder) *RecallService {
	return &RecallService{store: store, embedder: embedder}
}

func (s *RecallService) Recall(ctx context.Context, session entity.Session, query entity.RecallQuery) (entity.RecallResult, error) {
	if s.store == nil {
		return entity.RecallResult{}, errors.New("历史检索存储未配置")
	}
	ctx, cancel := context.WithTimeout(ctx, recallServiceTimeout)
	defer cancel()
	query = clampRecallQuery(query)
	if strings.TrimSpace(query.Text) == "" {
		return entity.RecallResult{}, errors.New("查询内容不能为空")
	}

	scope, scopePartial, err := s.resolveScope(ctx, session)
	if err != nil {
		return entity.RecallResult{}, err
	}
	if len(scope.SessionKeys) == 0 {
		return entity.RecallResult{Partial: true, Reasons: []string{"当前会话身份不完整，未执行跨会话检索"}}, nil
	}

	if query.From != nil && query.To != nil && *query.From >= *query.To {
		return entity.RecallResult{}, errors.New("from_time 必须早于 to_time")
	}
	if query.MinSimilarity != nil && (math.IsNaN(*query.MinSimilarity) || math.IsInf(*query.MinSimilarity, 0) || *query.MinSimilarity < -1 || *query.MinSimilarity > 1) {
		return entity.RecallResult{}, errors.New("min_similarity 必须在 -1 到 1 之间")
	}

	result := entity.RecallResult{}
	if scopePartial {
		result.Partial = true
		result.Reasons = append(result.Reasons, "无法完整确认当前群成员，结果仅覆盖本群历史")
	}
	var queryVector []float32
	if s.embedder == nil {
		result.Reasons = append(result.Reasons, "未配置 embedding 模型，本次仅进行关键词检索")
	} else {
		embedCtx, cancel := context.WithTimeout(ctx, recallCallTimeout)
		vectors, embedErr := s.embedder.Embed(embedCtx, []string{query.Text})
		cancel()
		if embedErr != nil {
			result.Partial = true
			result.Reasons = append(result.Reasons, "语义检索暂不可用，当前仅使用关键词结果")
			logger.From(ctx).Warn("记忆召回 embedding 失败，当前仅使用关键词结果")
		} else if len(vectors) != 1 || !validVector(vectors[0]) {
			result.Partial = true
			result.Reasons = append(result.Reasons, "语义检索模型返回了无效向量，当前仅使用关键词结果")
		} else {
			queryVector = vectors[0]
		}
	}

	coverage, err := s.store.RecallCoverage(ctx, scope, query, modelKey(s.embedder), modelDimensions(s.embedder))
	if err != nil {
		return entity.RecallResult{}, fmt.Errorf("检查记忆索引状态失败: %w", err)
	}
	if coverage.Indexing {
		result.Partial = true
		result.Reasons = append(result.Reasons, "部分历史仍在建立检索索引")
	}
	if (query.From != nil || query.To != nil) && coverage.UnknownTime {
		result.Partial = true
		result.Reasons = append(result.Reasons, "部分旧摘要没有可确认的对话发生时间，未纳入时间筛选")
	}
	if (query.From != nil || query.To != nil) && coverage.BoundaryExcluded {
		result.Partial = true
		result.Reasons = append(result.Reasons, "跨越时间边界的历史片段未纳入精确时间筛选")
	}

	lexical, err := s.store.SearchRecallLexical(ctx, scope, query, query.Candidates)
	if err != nil {
		return entity.RecallResult{}, fmt.Errorf("关键词检索失败: %w", err)
	}
	var vectorCandidates []entity.RecallCandidate
	if len(queryVector) > 0 {
		vectorCandidates, err = s.store.SearchRecallVectors(ctx, scope, query, queryVector, modelKey(s.embedder), query.Candidates)
		if err != nil {
			result.Partial = true
			result.Reasons = append(result.Reasons, "向量检索暂不可用，当前仅使用关键词结果")
			logger.From(ctx).Warn("记忆召回向量查询失败", logger.Err(err))
		}
	}
	merged := reciprocalRankFusion(lexical, vectorCandidates)
	if len(merged) > query.Candidates {
		merged = merged[:query.Candidates]
	}
	if len(merged) == 0 {
		result.Reasons = append(result.Reasons, "当前已索引历史中没有找到相关内容")
		return result, nil
	}
	loaded, err := s.store.LoadRecallContext(ctx, scope, merged, query)
	if err != nil {
		return entity.RecallResult{}, fmt.Errorf("读取召回上下文失败: %w", err)
	}
	result.Items = mergeRecallItems(loaded, query.Limit)
	if len(result.Items) == 0 {
		result.Reasons = append(result.Reasons, "当前已索引历史中没有找到相关内容")
	}
	return result, nil
}

func (s *RecallService) resolveScope(ctx context.Context, session entity.Session) (entity.RecallScope, bool, error) {
	keys := make(map[string]struct{})
	if session.GroupID != "" {
		keys[session.GroupID] = struct{}{}
		reader, ok := domain.GroupMemberReaderFrom(ctx)
		if !ok {
			return entity.RecallScope{SessionKeys: []string{session.GroupID}}, true, nil
		}
		members, err := reader.ListCurrentMembers(ctx)
		if err != nil {
			return entity.RecallScope{SessionKeys: []string{session.GroupID}}, true, nil
		}
		partial := len(members) > recallMaxScopeMembers
		if partial {
			members = members[:recallMaxScopeMembers]
		}
		for _, uid := range members {
			if !isQQID(uid) {
				partial = true
				continue
			}
			keys["private:"+uid] = struct{}{}
		}
		return sortedRecallScope(keys), partial, nil
	}
	if !isQQID(session.UserID) {
		return entity.RecallScope{}, true, nil
	}
	keys["private:"+session.UserID] = struct{}{}
	return sortedRecallScope(keys), false, nil
}

func isQQID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	n, err := strconv.ParseInt(id, 10, 64)
	return err == nil && n > 0
}

func sortedRecallScope(keys map[string]struct{}) entity.RecallScope {
	out := entity.RecallScope{SessionKeys: make([]string, 0, len(keys))}
	for key := range keys {
		out.SessionKeys = append(out.SessionKeys, key)
	}
	sort.Strings(out.SessionKeys)
	return out
}

func clampRecallQuery(q entity.RecallQuery) entity.RecallQuery {
	if q.Limit <= 0 {
		q.Limit = recallDefaultLimit
	}
	if q.Limit > recallMaxLimit {
		q.Limit = recallMaxLimit
	}
	if q.MaxMessages <= 0 {
		q.MaxMessages = recallDefaultMaxMessages
	}
	if q.MaxMessages > recallHardMaxMessages {
		q.MaxMessages = recallHardMaxMessages
	}
	if q.Candidates <= 0 {
		q.Candidates = recallDefaultCandidates
	}
	if q.Candidates > recallMaxCandidates {
		q.Candidates = recallMaxCandidates
	}
	if utf8.RuneCountInString(q.Text) > recallMaxQueryRunes {
		r := []rune(q.Text)
		q.Text = string(r[:recallMaxQueryRunes])
	}
	return q
}

func modelKey(embedder domain.Embedder) string {
	if embedder == nil {
		return ""
	}
	return embedder.ModelKey()
}

func modelDimensions(embedder domain.Embedder) int {
	if embedder == nil {
		return 0
	}
	return embedder.Dimensions()
}

func validVector(v []float32) bool {
	if len(v) == 0 {
		return false
	}
	norm := float64(0)
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return false
		}
		norm += float64(x) * float64(x)
	}
	return norm > 0
}

func reciprocalRankFusion(lexical, vectors []entity.RecallCandidate) []entity.RecallCandidate {
	type ranked struct {
		candidate entity.RecallCandidate
		score     float64
	}
	byID := make(map[int64]*ranked, len(lexical)+len(vectors))
	for _, candidates := range [2][]entity.RecallCandidate{lexical, vectors} {
		for i, c := range candidates {
			rank := i + 1
			if c.Rank > 0 {
				rank = c.Rank
			}
			r := byID[c.Document.ID]
			if r == nil {
				r = &ranked{candidate: c}
				byID[c.Document.ID] = r
			}
			r.score += 1 / (rrfConstant + float64(rank))
		}
	}
	all := make([]*ranked, 0, len(byID))
	for _, r := range byID {
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].score == all[j].score {
			return all[i].candidate.Document.ID < all[j].candidate.Document.ID
		}
		return all[i].score > all[j].score
	})
	out := make([]entity.RecallCandidate, len(all))
	for i, r := range all {
		out[i] = r.candidate
		out[i].Score = r.score
	}
	return out
}

func mergeRecallItems(candidates []entity.RecallCandidate, limit int) []entity.RecallItem {
	items := make([]entity.RecallItem, 0, limit)
	seenMessages := make(map[int64]struct{})
	seenSummaries := make(map[string]struct{})
	for _, c := range candidates {
		doc := c.Document
		if doc.Kind == entity.RecallSourceSummary {
			key := doc.SessionKey + "\x00" + strconv.FormatInt(valueOrZero(doc.EventFrom), 10) + "\x00" + doc.Text
			if _, ok := seenSummaries[key]; ok {
				continue
			}
			seenSummaries[key] = struct{}{}
			items = append(items, entity.RecallItem{Text: doc.Text, EventFrom: doc.EventFrom, EventTo: doc.EventTo})
		} else {
			msgs := make([]entity.RecallMessage, 0, len(c.Messages))
			for _, msg := range c.Messages {
				if _, exists := seenMessages[msg.ID]; exists {
					continue
				}
				seenMessages[msg.ID] = struct{}{}
				msgs = append(msgs, msg)
			}
			if len(msgs) == 0 {
				continue
			}
			items = append(items, entity.RecallItem{Messages: msgs, EventFrom: doc.EventFrom, EventTo: doc.EventTo})
		}
		if len(items) >= limit {
			break
		}
	}
	return items
}

func valueOrZero(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

// RecallIndexer 以可重入、可恢复的批处理维护 FTS 与 embedding 派生索引。
type RecallIndexer struct {
	store    domain.RecallIndexStore
	embedder domain.Embedder

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRecallIndexer(store domain.RecallIndexStore, embedder domain.Embedder) *RecallIndexer {
	return &RecallIndexer{store: store, embedder: embedder}
}

func (i *RecallIndexer) Start(parent context.Context) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cancel != nil || i.store == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	i.cancel = cancel
	i.done = make(chan struct{})
	go func() {
		defer close(i.done)
		i.run(ctx)
	}()
}

func (i *RecallIndexer) Close() error {
	i.mu.Lock()
	cancel, done := i.cancel, i.done
	i.cancel, i.done = nil, nil
	i.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return nil
}

func (i *RecallIndexer) run(ctx context.Context) {
	backoff := recallIndexInterval
	wait := func(duration time.Duration) bool {
		timer := time.NewTimer(duration)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return true
		}
	}
	for {
		progress, sourceErr := i.indexSources(ctx)
		failed := sourceErr != nil
		if sourceErr != nil {
			logger.Warn("记忆召回文本索引更新失败", logger.Err(sourceErr))
		}
		if i.embedder != nil {
			vectorProgress, vectorErr := i.indexVectors(ctx)
			progress = progress || vectorProgress
			if vectorErr != nil {
				failed = true
				// embedding provider 的错误可能包含响应正文；日志只记录生命周期，不写明细。
				logger.Warn("记忆召回向量索引更新失败，后台将重试")
			}
		}
		if failed {
			if !wait(backoff) {
				return
			}
			backoff *= 2
			if backoff > recallIndexMaxBackoff {
				backoff = recallIndexMaxBackoff
			}
			continue
		}
		backoff = recallIndexInterval
		if progress {
			if !wait(recallIndexBatchInterval) {
				return
			}
			continue
		}
		if !wait(recallIndexInterval) {
			return
		}
	}
}

func (i *RecallIndexer) indexSources(ctx context.Context) (bool, error) {
	sources, err := i.store.ListDirtyRecallSources(ctx, recallIndexBatch)
	if err != nil {
		return false, err
	}
	progress := false
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return progress, err
		}
		docs := buildRecallDocuments(source)
		applied, err := i.store.ReplaceRecallDocuments(ctx, source, docs)
		if err != nil {
			return progress, err
		}
		progress = progress || applied
	}
	return progress, nil
}

func (i *RecallIndexer) indexVectors(ctx context.Context) (bool, error) {
	docs, err := i.store.ListPendingRecallVectors(ctx, i.embedder.ModelKey(), i.embedder.Dimensions(), recallEmbedBatch)
	if err != nil || len(docs) == 0 {
		return false, err
	}
	texts := make([]string, len(docs))
	for n := range docs {
		texts[n] = docs[n].Text
	}
	callCtx, cancel := context.WithTimeout(ctx, recallCallTimeout)
	vectors, err := i.embedder.Embed(callCtx, texts)
	cancel()
	if err != nil {
		return false, err
	}
	if len(vectors) != len(docs) {
		return false, fmt.Errorf("embedding 返回 %d 个向量，与 %d 条文档不匹配", len(vectors), len(docs))
	}
	out := make([]entity.RecallVector, len(docs))
	dimensions := 0
	for n, vector := range vectors {
		if !validVector(vector) {
			return false, fmt.Errorf("embedding 第 %d 个向量无效", n)
		}
		if dimensions == 0 {
			dimensions = len(vector)
		} else if len(vector) != dimensions {
			return false, errors.New("embedding 单批返回了不同维度的向量")
		}
		out[n] = entity.RecallVector{DocumentID: docs[n].ID, ContentVersion: docs[n].Version, ModelKey: i.embedder.ModelKey(), Values: vector}
	}
	if err := i.store.SaveRecallVectors(ctx, out); err != nil {
		return false, err
	}
	return true, nil
}

func buildRecallDocuments(source entity.RecallSource) []entity.RecallDocument {
	var text string
	var from, to *int64
	switch source.Kind {
	case entity.RecallSourceMessage:
		if source.Message == nil || source.SessionKey == "" {
			return nil
		}
		text = source.Message.ForLLM()
		if source.Message.MessageType == "group" && source.Message.UserID != "" {
			text = "[" + source.Message.UserID + "]: " + text
		}
		if source.Message.Timestamp > 0 {
			from, to = new(source.Message.Timestamp), new(source.Message.Timestamp)
		}
	case entity.RecallSourceSummary:
		if source.Summary == nil || source.SessionKey == "" {
			return nil
		}
		parts := []string{source.Summary.Text}
		if len(source.Summary.Keywords) > 0 {
			parts = append(parts, "关键词："+strings.Join(source.Summary.Keywords, "、"))
		}
		if len(source.Summary.Decisions) > 0 {
			parts = append(parts, "决定："+strings.Join(source.Summary.Decisions, "；"))
		}
		text = strings.Join(parts, "\n")
		from, to = source.Summary.EventFrom, source.Summary.EventTo
	default:
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	var docs []entity.RecallDocument
	for start, segment := 0, 0; start < len(runes); segment++ {
		end := min(start+recallDocumentRunes, len(runes))
		body := string(runes[start:end])
		hash := sha256.Sum256([]byte(body))
		docs = append(docs, entity.RecallDocument{
			Kind: source.Kind, SourceID: source.ID, SegmentNo: segment,
			SessionKey: source.SessionKey, Version: source.Version, Text: body,
			ContentHash: hex.EncodeToString(hash[:]), FromRune: start, ToRune: end,
			EventFrom: from, EventTo: to,
		})
		start = end
	}
	return docs
}
