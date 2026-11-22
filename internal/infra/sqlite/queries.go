package sqlite

const (
	// ── messages ──

	sqlSaveMessage = `INSERT OR IGNORE INTO messages (message_id, group_id, user_id, parts, timestamp, message_type, session_key)
	 VALUES (?, ?, ?, ?, ?, ?, ?)`

	sqlGetMessages = `SELECT message_id, group_id, user_id, parts, timestamp, message_type
	 FROM messages WHERE session_key = ? ORDER BY timestamp DESC LIMIT ? OFFSET ?`

	sqlUpdateMessageParts = `UPDATE messages SET parts = ?, content_version = content_version + 1 WHERE message_id = ?`

	// ── conversation_summary ──

	sqlSaveSummary = `INSERT INTO conversation_summary (chat_id, seq, text, keywords, decisions, created_at, event_from, event_to)
	 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	 ON CONFLICT(chat_id, seq) DO UPDATE SET
	 text=excluded.text, keywords=excluded.keywords, decisions=excluded.decisions, created_at=excluded.created_at,
	 event_from=excluded.event_from, event_to=excluded.event_to, content_version=conversation_summary.content_version+1`

	sqlListSummaries = `SELECT chat_id, seq, text, keywords, decisions, created_at, event_from, event_to
	 FROM conversation_summary WHERE chat_id = ? ORDER BY seq DESC LIMIT ?`

	// ── group_profile ──

	sqlUpsertGroupProfile = `INSERT INTO group_profile (group_id, culture, topics, active_hours, rules, atmosphere)
	 VALUES (?, ?, ?, ?, ?, ?)
	 ON CONFLICT(group_id) DO UPDATE SET
	 culture=excluded.culture, topics=excluded.topics, active_hours=excluded.active_hours,
	 rules=excluded.rules, atmosphere=excluded.atmosphere`

	sqlGetGroupProfile = `SELECT group_id, culture, topics, active_hours, rules, atmosphere
	 FROM group_profile WHERE group_id = ?`

	// ── group_jargon ──

	sqlAddJargon    = `INSERT OR IGNORE INTO group_jargon (group_id, jargon) VALUES (?, ?)`
	sqlListJargon   = `SELECT jargon FROM group_jargon WHERE group_id = ?`
	sqlDeleteJargon = `DELETE FROM group_jargon WHERE group_id = ? AND jargon = ?`

	sqlListConfirmedJargon = `SELECT jargon FROM group_jargon WHERE group_id = ? AND status = 'confirmed'`

	sqlConfirmJargon = `UPDATE group_jargon SET status = 'confirmed' WHERE group_id = ? AND jargon = ?`

	// ── member_facts ──

	sqlAddMemberFact    = `INSERT OR IGNORE INTO member_facts (group_id, user_id, fact) VALUES (?, ?, ?)`
	sqlListMemberFacts  = `SELECT fact FROM member_facts WHERE group_id = ? AND user_id = ?`
	sqlDeleteMemberFact = `DELETE FROM member_facts WHERE group_id = ? AND user_id = ? AND fact = ?`

	// ── persona ──

	sqlInsertPersona = `INSERT INTO persona (agent, name, system_prompt) VALUES (?, ?, ?)`
	sqlUpdatePersona = `UPDATE persona SET agent=?, name=?, system_prompt=? WHERE id=?`
	sqlGetPersona    = `SELECT id, agent, name, system_prompt FROM persona WHERE id = ?`

	sqlGetPersonaByAgent = `SELECT id, agent, name, system_prompt FROM persona WHERE agent = ?`

	// ── bot_state ──

	sqlUpsertBotState = `INSERT INTO bot_state (group_id, state) VALUES (?, ?)
	 ON CONFLICT(group_id) DO UPDATE SET state=excluded.state`

	sqlGetBotState = `SELECT group_id, state FROM bot_state WHERE group_id = ?`

	// ── group_config ──

	sqlUpsertGroupConfig = `INSERT INTO group_config (group_id, mode, energy_max, energy_cost, energy_recover,
	 energy_threshold, cooldown_seconds, consecutive_limit, rest_seconds, quiet_hours_start, quiet_hours_end, short_message_chars,
	 group_mgmt_enabled)
	 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	 ON CONFLICT(group_id) DO UPDATE SET
	 mode=excluded.mode, energy_max=excluded.energy_max, energy_cost=excluded.energy_cost,
	 energy_recover=excluded.energy_recover, energy_threshold=excluded.energy_threshold,
	 cooldown_seconds=excluded.cooldown_seconds, consecutive_limit=excluded.consecutive_limit,
	 rest_seconds=excluded.rest_seconds, quiet_hours_start=excluded.quiet_hours_start,
	 quiet_hours_end=excluded.quiet_hours_end, short_message_chars=excluded.short_message_chars,
	 group_mgmt_enabled=excluded.group_mgmt_enabled`

	sqlGetGroupConfig = `SELECT group_id, mode, energy_max, energy_cost, energy_recover,
	 energy_threshold, cooldown_seconds, consecutive_limit, rest_seconds, quiet_hours_start, quiet_hours_end, short_message_chars,
	 group_mgmt_enabled
	 FROM group_config WHERE group_id = ?`

	// ── admin 管理后端（P7-001）──

	// ── group_config (admin) ──

	sqlListGroupConfigs = `SELECT group_id, mode, energy_max, energy_cost, energy_recover,
	 energy_threshold, cooldown_seconds, consecutive_limit, rest_seconds, quiet_hours_start, quiet_hours_end, short_message_chars,
	 group_mgmt_enabled FROM group_config ORDER BY group_id`

	sqlDeleteGroupConfig = `DELETE FROM group_config WHERE group_id = ?`

	// ── persona (admin) ──

	sqlListPersonas = `SELECT id, agent, name, system_prompt FROM persona ORDER BY id`

	sqlUpsertPersona = `INSERT INTO persona (agent, name, system_prompt) VALUES (?, ?, ?)
	 ON CONFLICT(agent) DO UPDATE SET name=excluded.name, system_prompt=excluded.system_prompt`

	// ── group_jargon (admin) ──

	sqlListJargonWithStatus = `SELECT group_id, jargon, status FROM group_jargon WHERE group_id = ? ORDER BY id`

	// ── group_profile (admin) ──

	sqlDeleteGroupProfile = `DELETE FROM group_profile WHERE group_id = ?`

	// ── admin_user ──

	sqlGetAdminUserByName = `SELECT id, username, password_hash, created_at, updated_at FROM admin_user WHERE username = ?`

	sqlCreateAdminUser = `INSERT INTO admin_user (username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)`

	sqlListAdminUsers = `SELECT id, username, password_hash, created_at, updated_at FROM admin_user ORDER BY id`

	sqlUpdateAdminUserPassword = `UPDATE admin_user SET password_hash = ?, updated_at = strftime('%s','now') WHERE username = ?`

	// ── memory recall index ──

	sqlListDirtyRecallSources = `
SELECT 'message', m.id, m.session_key, m.content_version, m.message_id, m.group_id, m.user_id, m.parts, m.timestamp, m.message_type,
       '', '', '', NULL, NULL
FROM messages m
LEFT JOIN recall_indexed_sources i ON i.source_kind='message' AND i.source_id=m.id
WHERE m.session_key <> '' AND COALESCE(i.source_version, 0) < m.content_version
UNION ALL
SELECT 'summary', s.id, s.chat_id, s.content_version, '', '', '', '[]', s.created_at, 'summary',
       s.text, s.keywords, s.decisions, s.event_from, s.event_to
FROM conversation_summary s
LEFT JOIN recall_indexed_sources i ON i.source_kind='summary' AND i.source_id=s.id
WHERE COALESCE(i.source_version, 0) < s.content_version
ORDER BY 2, 1 LIMIT ?`

	sqlDeleteRecallFTSBySource = `DELETE FROM recall_fts WHERE rowid IN (
 SELECT id FROM recall_documents WHERE source_kind=? AND source_id=?)`
	sqlDeleteRecallDocumentMessagesBySource = `DELETE FROM recall_document_messages WHERE document_id IN (
	 SELECT id FROM recall_documents WHERE source_kind=? AND source_id=?)`
	sqlDeleteRecallVectorsBySource = `DELETE FROM recall_vectors WHERE document_id IN (
	 SELECT id FROM recall_documents WHERE source_kind=? AND source_id=?)`
	sqlDeleteRecallDocsBySource = `DELETE FROM recall_documents WHERE source_kind=? AND source_id=?`
	sqlInsertRecallDocument     = `INSERT INTO recall_documents
 (session_key, source_kind, source_id, segment_no, source_version, content, content_hash, event_from, event_to)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	sqlInsertRecallFTS             = `INSERT INTO recall_fts(rowid, content) VALUES (?, ?)`
	sqlInsertRecallDocumentMessage = `INSERT INTO recall_document_messages
 (document_id, message_row_id, source_version, position, from_rune, to_rune) VALUES (?, ?, ?, ?, ?, ?)`
	sqlUpsertRecallIndexedSource = `INSERT INTO recall_indexed_sources(source_kind, source_id, source_version, indexed_at)
 VALUES (?, ?, ?, strftime('%s','now'))
 ON CONFLICT(source_kind, source_id) DO UPDATE SET source_version=excluded.source_version, indexed_at=excluded.indexed_at`

	sqlListPendingRecallVectors = `SELECT d.id, d.source_kind, d.source_id, d.segment_no, d.session_key, d.source_version, d.content, d.event_from, d.event_to
FROM recall_documents d
LEFT JOIN recall_vectors v ON v.document_id=d.id AND v.model_key=? AND v.content_version=d.source_version AND (?<=0 OR v.dimensions=?)
WHERE v.document_id IS NULL ORDER BY d.id LIMIT ?`
	sqlUpsertRecallVector = `INSERT INTO recall_vectors(document_id, model_key, content_version, dimensions, vector)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(document_id, model_key) DO UPDATE SET content_version=excluded.content_version, dimensions=excluded.dimensions, vector=excluded.vector
WHERE EXISTS (SELECT 1 FROM recall_documents d WHERE d.id=excluded.document_id AND d.source_version=excluded.content_version)`

	sqlRecallLexical = `SELECT d.id, d.session_key, d.source_kind, d.source_id, d.segment_no, d.source_version, d.content, d.event_from, d.event_to, bm25(recall_fts)
FROM recall_fts JOIN recall_documents d ON d.id=recall_fts.rowid
WHERE recall_fts MATCH ?
 AND d.session_key IN (SELECT value FROM json_each(?))
 AND ((? IS NULL AND ? IS NULL) OR (d.event_from IS NOT NULL AND d.event_to IS NOT NULL AND (? IS NULL OR d.event_from >= ?) AND (? IS NULL OR d.event_to < ?)))
 AND ((d.source_kind='message' AND EXISTS(SELECT 1 FROM messages m WHERE m.id=d.source_id AND m.content_version=d.source_version))
   OR (d.source_kind='summary' AND EXISTS(SELECT 1 FROM conversation_summary s WHERE s.id=d.source_id AND s.content_version=d.source_version)))
ORDER BY bm25(recall_fts) LIMIT ?`

	sqlRecallSubstring = `SELECT d.id, d.session_key, d.source_kind, d.source_id, d.segment_no, d.source_version, d.content, d.event_from, d.event_to, 0.0
FROM recall_documents d
WHERE instr(d.content, ?) > 0
 AND d.session_key IN (SELECT value FROM json_each(?))
 AND ((? IS NULL AND ? IS NULL) OR (d.event_from IS NOT NULL AND d.event_to IS NOT NULL AND (? IS NULL OR d.event_from >= ?) AND (? IS NULL OR d.event_to < ?)))
 AND ((d.source_kind='message' AND EXISTS(SELECT 1 FROM messages m WHERE m.id=d.source_id AND m.content_version=d.source_version))
   OR (d.source_kind='summary' AND EXISTS(SELECT 1 FROM conversation_summary s WHERE s.id=d.source_id AND s.content_version=d.source_version)))
ORDER BY d.event_from DESC, d.id DESC LIMIT ?`

	sqlRecallCoverageWithVectors = `SELECT
	 EXISTS(SELECT 1 FROM messages m LEFT JOIN recall_indexed_sources i ON i.source_kind='message' AND i.source_id=m.id WHERE m.session_key IN (SELECT value FROM json_each(?)) AND COALESCE(i.source_version,0)<m.content_version)
	 OR EXISTS(SELECT 1 FROM conversation_summary s LEFT JOIN recall_indexed_sources i ON i.source_kind='summary' AND i.source_id=s.id WHERE s.chat_id IN (SELECT value FROM json_each(?)) AND COALESCE(i.source_version,0)<s.content_version)
	 OR (? <> '' AND EXISTS(SELECT 1 FROM recall_documents d LEFT JOIN recall_vectors v ON v.document_id=d.id AND v.model_key=? AND v.content_version=d.source_version AND (?<=0 OR v.dimensions=?) WHERE d.session_key IN (SELECT value FROM json_each(?)) AND v.document_id IS NULL)),
	 EXISTS(SELECT 1 FROM recall_documents d WHERE d.session_key IN (SELECT value FROM json_each(?)) AND (d.event_from IS NULL OR d.event_to IS NULL)),
	 EXISTS(SELECT 1 FROM recall_documents d WHERE d.session_key IN (SELECT value FROM json_each(?))
	 AND ((? IS NOT NULL AND d.event_from < ? AND d.event_to >= ?)
		 OR (? IS NOT NULL AND d.event_from < ? AND d.event_to >= ?))
	 AND d.event_from IS NOT NULL AND d.event_to IS NOT NULL)`

	sqlRecallVectorWithDimensions = `WITH scoped AS MATERIALIZED (
	 SELECT d.id, d.session_key, d.source_kind, d.source_id, d.segment_no, d.source_version, d.content, d.event_from, d.event_to, v.vector
	 FROM recall_documents d JOIN recall_vectors v ON v.document_id=d.id AND v.model_key=? AND v.content_version=d.source_version
	 WHERE d.session_key IN (SELECT value FROM json_each(?)) AND v.dimensions=?
	  AND ((? IS NULL AND ? IS NULL) OR (d.event_from IS NOT NULL AND d.event_to IS NOT NULL AND (? IS NULL OR d.event_from >= ?) AND (? IS NULL OR d.event_to < ?)))
	  AND ((d.source_kind='message' AND EXISTS(SELECT 1 FROM messages m WHERE m.id=d.source_id AND m.content_version=d.source_version))
	    OR (d.source_kind='summary' AND EXISTS(SELECT 1 FROM conversation_summary s WHERE s.id=d.source_id AND s.content_version=d.source_version)))
	), scored AS MATERIALIZED (
	 SELECT id, session_key, source_kind, source_id, segment_no, source_version, content, event_from, event_to,
	        vec_distance_cosine(vector, ?) AS distance
	 FROM scoped
	)
SELECT id, session_key, source_kind, source_id, segment_no, source_version, content, event_from, event_to, distance
FROM scored WHERE (? IS NULL OR 1.0-distance >= ?)
ORDER BY distance LIMIT ?`

	sqlRecallAnchor = `SELECT timestamp, user_id, parts, message_type FROM messages WHERE id=? AND session_key=?`
	sqlRecallBefore = `SELECT id, timestamp, user_id, parts FROM messages
WHERE session_key=? AND (timestamp < ? OR (timestamp=? AND id < ?))
 AND (? IS NULL OR timestamp >= ?) AND (? IS NULL OR timestamp < ?)
ORDER BY timestamp DESC, id DESC LIMIT ?`
	sqlRecallAfter = `SELECT id, timestamp, user_id, parts FROM messages
WHERE session_key=? AND (timestamp > ? OR (timestamp=? AND id > ?))
 AND (? IS NULL OR timestamp >= ?) AND (? IS NULL OR timestamp < ?)
ORDER BY timestamp, id LIMIT ?`

	sqlRecallMessageVersion = `SELECT content_version FROM messages WHERE id=? AND session_key=?`
	sqlRecallSummaryVersion = `SELECT content_version FROM conversation_summary WHERE id=? AND chat_id=?`
)
