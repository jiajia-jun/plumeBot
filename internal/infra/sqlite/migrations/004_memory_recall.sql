-- Migration: 004_memory_recall
-- 增加显式会话归属、摘要事件时间及本地 RAG 派生索引。
-- 不删除任何历史聊天/摘要；无法确认归属的旧私聊 bot 回复保持 session_key 为空。

ALTER TABLE messages ADD COLUMN session_key TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN content_version INTEGER NOT NULL DEFAULT 1;
UPDATE messages
SET session_key = group_id
WHERE group_id <> '' AND session_key = '';
UPDATE messages
SET session_key = 'private:' || user_id
WHERE group_id = ''
  AND message_type = 'private'
  AND user_id <> ''
  AND message_id NOT LIKE 'self:%'
  AND session_key = '';
CREATE INDEX IF NOT EXISTS idx_messages_session_time
    ON messages(session_key, timestamp, id);

ALTER TABLE conversation_summary ADD COLUMN event_from INTEGER;
ALTER TABLE conversation_summary ADD COLUMN event_to INTEGER;
ALTER TABLE conversation_summary ADD COLUMN content_version INTEGER NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS recall_indexed_sources (
    source_kind TEXT NOT NULL,
    source_id INTEGER NOT NULL,
    source_version INTEGER NOT NULL,
    indexed_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(source_kind, source_id)
);

CREATE TABLE IF NOT EXISTS recall_documents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_key TEXT NOT NULL,
    source_kind TEXT NOT NULL,
    source_id INTEGER NOT NULL,
    segment_no INTEGER NOT NULL DEFAULT 0,
    source_version INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    event_from INTEGER,
    event_to INTEGER,
    UNIQUE(source_kind, source_id, segment_no)
);
CREATE INDEX IF NOT EXISTS idx_recall_documents_session_time
    ON recall_documents(session_key, event_from, id);
CREATE INDEX IF NOT EXISTS idx_recall_documents_source
    ON recall_documents(source_kind, source_id, source_version);

CREATE TABLE IF NOT EXISTS recall_document_messages (
    document_id INTEGER NOT NULL REFERENCES recall_documents(id) ON DELETE CASCADE,
    message_row_id INTEGER NOT NULL,
    source_version INTEGER NOT NULL,
    position INTEGER NOT NULL,
    from_rune INTEGER NOT NULL DEFAULT 0,
    to_rune INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(document_id, message_row_id, position)
);
CREATE INDEX IF NOT EXISTS idx_recall_document_messages_message
    ON recall_document_messages(message_row_id, source_version);

CREATE VIRTUAL TABLE IF NOT EXISTS recall_fts USING fts5(content, tokenize='trigram');

CREATE TABLE IF NOT EXISTS recall_vectors (
    document_id INTEGER NOT NULL REFERENCES recall_documents(id) ON DELETE CASCADE,
    model_key TEXT NOT NULL,
    content_version INTEGER NOT NULL,
    dimensions INTEGER NOT NULL,
    vector BLOB NOT NULL,
    PRIMARY KEY(document_id, model_key)
);
