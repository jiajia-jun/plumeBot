package sqlite

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"plumebot/internal/domain"
	"plumebot/internal/domain/entity"
)

var _ domain.RecallStore = (*Storage)(nil)
var _ domain.RecallIndexStore = (*Storage)(nil)

func (s *Storage) ListDirtyRecallSources(ctx context.Context, limit int) ([]entity.RecallSource, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 256 {
		limit = 256
	}
	rows, err := s.db.QueryContext(ctx, sqlListDirtyRecallSources, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.RecallSource
	for rows.Next() {
		var source entity.RecallSource
		var messageID, groupID, userID, parts, messageType string
		var timestamp int64
		var eventFrom, eventTo sql.NullInt64
		var summaryText, keywords, decisions string
		if err := rows.Scan(&source.Kind, &source.ID, &source.SessionKey, &source.Version,
			&messageID, &groupID, &userID, &parts, &timestamp, &messageType,
			&summaryText, &keywords, &decisions, &eventFrom, &eventTo); err != nil {
			return nil, err
		}
		switch source.Kind {
		case entity.RecallSourceMessage:
			msg := entity.Message{
				MessageID: messageID, GroupID: groupID, UserID: userID,
				Timestamp: timestamp, MessageType: messageType,
			}
			if err := json.Unmarshal([]byte(parts), &msg.Parts); err != nil {
				return nil, fmt.Errorf("解析历史消息 %d 的 parts 失败: %w", source.ID, err)
			}
			source.Message = &msg
		case entity.RecallSourceSummary:
			sum := entity.Summary{ChatID: source.SessionKey, Seq: source.ID, Text: summaryText}
			if err := json.Unmarshal([]byte(keywords), &sum.Keywords); err != nil {
				return nil, fmt.Errorf("解析归档摘要 %d 的 keywords 失败: %w", source.ID, err)
			}
			if err := json.Unmarshal([]byte(decisions), &sum.Decisions); err != nil {
				return nil, fmt.Errorf("解析归档摘要 %d 的 decisions 失败: %w", source.ID, err)
			}
			// 重新读取摘要真实时间范围；Scan 的 NULL 指针由下面的 helper 承载。
			if eventFrom.Valid {
				sum.EventFrom = &eventFrom.Int64
			}
			if eventTo.Valid {
				sum.EventTo = &eventTo.Int64
			}
			source.Summary = &sum
		}
		out = append(out, source)
	}
	return out, rows.Err()
}

func (s *Storage) ReplaceRecallDocuments(ctx context.Context, source entity.RecallSource, docs []entity.RecallDocument) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var current int64
	var query string
	switch source.Kind {
	case entity.RecallSourceMessage:
		query = sqlRecallMessageVersion
		if err := tx.QueryRowContext(ctx, query, source.ID, source.SessionKey).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
	case entity.RecallSourceSummary:
		query = sqlRecallSummaryVersion
		if err := tx.QueryRowContext(ctx, query, source.ID, source.SessionKey).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
	default:
		return false, fmt.Errorf("未知召回来源类型 %q", source.Kind)
	}
	if current != source.Version {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, sqlDeleteRecallFTSBySource, source.Kind, source.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, sqlDeleteRecallDocumentMessagesBySource, source.Kind, source.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, sqlDeleteRecallVectorsBySource, source.Kind, source.ID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, sqlDeleteRecallDocsBySource, source.Kind, source.ID); err != nil {
		return false, err
	}
	for _, doc := range docs {
		if doc.Kind != source.Kind || doc.SourceID != source.ID || doc.Version != source.Version || doc.SessionKey != source.SessionKey {
			return false, errors.New("召回文档来源与待索引记录不匹配")
		}
		res, err := tx.ExecContext(ctx, sqlInsertRecallDocument,
			doc.SessionKey, doc.Kind, doc.SourceID, doc.SegmentNo, doc.Version, doc.Text, doc.ContentHash, doc.EventFrom, doc.EventTo)
		if err != nil {
			return false, err
		}
		docID, err := res.LastInsertId()
		if err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, sqlInsertRecallFTS, docID, doc.Text); err != nil {
			return false, err
		}
		if source.Kind == entity.RecallSourceMessage {
			if _, err := tx.ExecContext(ctx, sqlInsertRecallDocumentMessage,
				docID, source.ID, source.Version, doc.SegmentNo, doc.FromRune, doc.ToRune); err != nil {
				return false, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertRecallIndexedSource, source.Kind, source.ID, source.Version); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Storage) ListPendingRecallVectors(ctx context.Context, modelKey string, dimensions, limit int) ([]entity.RecallDocument, error) {
	if modelKey == "" {
		return nil, nil
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 128 {
		limit = 128
	}
	rows, err := s.db.QueryContext(ctx, sqlListPendingRecallVectors, modelKey, dimensions, dimensions, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.RecallDocument
	for rows.Next() {
		var doc entity.RecallDocument
		if err := rows.Scan(&doc.ID, &doc.Kind, &doc.SourceID, &doc.SegmentNo, &doc.SessionKey,
			&doc.Version, &doc.Text, &doc.EventFrom, &doc.EventTo); err != nil {
			return nil, err
		}
		out = append(out, doc)
	}
	return out, rows.Err()
}

func (s *Storage) SaveRecallVectors(ctx context.Context, vectors []entity.RecallVector) error {
	if len(vectors) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, vector := range vectors {
		if vector.DocumentID <= 0 || vector.ContentVersion <= 0 || vector.ModelKey == "" || !validFloat32Vector(vector.Values) {
			return errors.New("召回向量字段无效")
		}
		blob := encodeFloat32Vector(vector.Values)
		if _, err := tx.ExecContext(ctx, sqlUpsertRecallVector,
			vector.DocumentID, vector.ModelKey, vector.ContentVersion, len(vector.Values), blob); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Storage) SearchRecallLexical(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, limit int) ([]entity.RecallCandidate, error) {
	if len(scope.SessionKeys) == 0 || limit <= 0 {
		return nil, nil
	}
	scopeJSON, err := json.Marshal(scope.SessionKeys)
	if err != nil {
		return nil, err
	}
	needle := strings.TrimSpace(query.Text)
	if needle == "" {
		return nil, nil
	}
	var rows *sql.Rows
	match := ftsMatchQuery(query.Text)
	if utf8.RuneCountInString(needle) < 3 || match == "" {
		rows, err = s.db.QueryContext(ctx, sqlRecallSubstring,
			needle, string(scopeJSON), query.From, query.To,
			query.From, query.From, query.To, query.To, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, sqlRecallLexical,
			match, string(scopeJSON), query.From, query.To,
			query.From, query.From, query.To, query.To, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecallCandidates(rows)
}

func (s *Storage) SearchRecallVectors(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, vector []float32, modelKey string, limit int) ([]entity.RecallCandidate, error) {
	if len(scope.SessionKeys) == 0 || len(vector) == 0 || modelKey == "" || limit <= 0 {
		return nil, nil
	}
	blob := encodeFloat32Vector(vector)
	scopeJSON, err := json.Marshal(scope.SessionKeys)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, sqlRecallVectorWithDimensions,
		modelKey, string(scopeJSON), len(vector), query.From, query.To, query.From, query.From, query.To, query.To,
		blob, query.MinSimilarity, query.MinSimilarity, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecallCandidates(rows)
}

func (s *Storage) RecallCoverage(ctx context.Context, scope entity.RecallScope, query entity.RecallQuery, modelKey string, dimensions int) (entity.RecallCoverage, error) {
	if len(scope.SessionKeys) == 0 {
		return entity.RecallCoverage{}, nil
	}
	encoded, err := json.Marshal(scope.SessionKeys)
	if err != nil {
		return entity.RecallCoverage{}, err
	}
	var coverage entity.RecallCoverage
	if err := s.db.QueryRowContext(ctx, sqlRecallCoverageWithVectors,
		string(encoded), string(encoded), modelKey, modelKey, dimensions, dimensions, string(encoded), string(encoded), string(encoded),
		query.From, query.From, query.From, query.To, query.To, query.To).Scan(
		&coverage.Indexing, &coverage.UnknownTime, &coverage.BoundaryExcluded); err != nil {
		return entity.RecallCoverage{}, err
	}
	if query.From == nil && query.To == nil {
		coverage.UnknownTime = false
		coverage.BoundaryExcluded = false
	}
	return coverage, nil
}

func (s *Storage) LoadRecallContext(ctx context.Context, scope entity.RecallScope, candidates []entity.RecallCandidate, query entity.RecallQuery) ([]entity.RecallCandidate, error) {
	allowed := make(map[string]struct{}, len(scope.SessionKeys))
	for _, key := range scope.SessionKeys {
		allowed[key] = struct{}{}
	}
	out := make([]entity.RecallCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		doc := candidate.Document
		if _, ok := allowed[doc.SessionKey]; !ok {
			continue
		}
		if doc.Kind != entity.RecallSourceMessage {
			out = append(out, candidate)
			continue
		}
		messages, err := s.loadMessageContext(ctx, doc, query)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return nil, err
		}
		candidate.Messages = messages
		out = append(out, candidate)
	}
	return out, nil
}

func (s *Storage) loadMessageContext(ctx context.Context, doc entity.RecallDocument, query entity.RecallQuery) ([]entity.RecallMessage, error) {
	var anchorTimestamp int64
	var userID, parts, messageType string
	if err := s.db.QueryRowContext(ctx, sqlRecallAnchor, doc.SourceID, doc.SessionKey).
		Scan(&anchorTimestamp, &userID, &parts, &messageType); err != nil {
		return nil, err
	}
	maxMessages := query.MaxMessages
	if maxMessages < 1 {
		maxMessages = 1
	}
	if maxMessages > 12 {
		maxMessages = 12
	}
	beforeN := (maxMessages - 1) / 2
	afterN := maxMessages - 1 - beforeN
	before, err := s.queryRecallNeighbors(ctx, sqlRecallBefore, doc.SessionKey, anchorTimestamp, doc.SourceID, query, beforeN)
	if err != nil {
		return nil, err
	}
	anchorText := renderRecallMessageText(userID, parts, messageType)
	if doc.SegmentNo >= 0 && doc.Text != "" {
		anchorText = doc.Text
	}
	anchor := entity.RecallMessage{ID: doc.SourceID, Timestamp: anchorTimestamp, UserID: userID, Text: anchorText}
	after, err := s.queryRecallNeighbors(ctx, sqlRecallAfter, doc.SessionKey, anchorTimestamp, doc.SourceID, query, afterN)
	if err != nil {
		return nil, err
	}
	for left, right := 0, len(before)-1; left < right; left, right = left+1, right-1 {
		before[left], before[right] = before[right], before[left]
	}
	out := append(before, anchor)
	out = append(out, after...)
	return out, nil
}

func (s *Storage) queryRecallNeighbors(ctx context.Context, querySQL, sessionKey string, timestamp, rowID int64, query entity.RecallQuery, limit int) ([]entity.RecallMessage, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, querySQL,
		sessionKey, timestamp, timestamp, rowID, query.From, query.From, query.To, query.To, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []entity.RecallMessage
	for rows.Next() {
		var msg entity.RecallMessage
		var parts string
		if err := rows.Scan(&msg.ID, &msg.Timestamp, &msg.UserID, &parts); err != nil {
			return nil, err
		}
		var content []entity.ContentPart
		if err := json.Unmarshal([]byte(parts), &content); err != nil {
			return nil, err
		}
		msg.Text = renderRecallMessageText(msg.UserID, parts, "group")
		if contentText := renderParts(content); contentText != "" {
			if msg.UserID == "" {
				msg.Text = contentText
			} else {
				msg.Text = "[" + msg.UserID + "]: " + contentText
			}
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func renderParts(parts []entity.ContentPart) string {
	return (entity.Message{Parts: parts}).ForLLM()
}

func renderRecallMessageText(userID, partsJSON, messageType string) string {
	var parts []entity.ContentPart
	if err := json.Unmarshal([]byte(partsJSON), &parts); err != nil {
		return "[历史消息内容无法读取]"
	}
	text := renderParts(parts)
	if messageType == "group" && userID != "" {
		return "[" + userID + "]: " + text
	}
	return text
}

func scanRecallCandidates(rows *sql.Rows) ([]entity.RecallCandidate, error) {
	var out []entity.RecallCandidate
	for rows.Next() {
		var candidate entity.RecallCandidate
		var doc entity.RecallDocument
		if err := rows.Scan(&doc.ID, &doc.SessionKey, &doc.Kind, &doc.SourceID, &doc.SegmentNo,
			&doc.Version, &doc.Text, &doc.EventFrom, &doc.EventTo, &candidate.Score); err != nil {
			return nil, err
		}
		doc.FromRune = 0
		doc.ToRune = utf8.RuneCountInString(doc.Text)
		candidate.Document = doc
		candidate.Rank = len(out) + 1
		out = append(out, candidate)
	}
	return out, rows.Err()
}

func ftsMatchQuery(query string) string {
	var terms []string
	var current []rune
	flush := func() {
		if len(current) >= 3 {
			for n := 0; n+3 <= len(current) && len(terms) < 16; n++ {
				terms = append(terms, `"`+string(current[n:n+3])+`"`)
			}
		}
		current = current[:0]
	}
	for _, r := range query {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current = append(current, r)
		} else {
			flush()
		}
	}
	flush()
	return strings.Join(terms, " OR ")
}

func encodeFloat32Vector(values []float32) []byte {
	blob := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(value))
	}
	return blob
}

func validFloat32Vector(values []float32) bool {
	if len(values) == 0 {
		return false
	}
	var norm float64
	for _, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return false
		}
		norm += float64(value) * float64(value)
	}
	return norm > 0
}

// ftsQueryTokenLength is kept as a compile-time guard for trigram query behavior.
const ftsQueryTokenLength = 3

var _ = ftsQueryTokenLength
var _ = utf8.RuneCountInString
