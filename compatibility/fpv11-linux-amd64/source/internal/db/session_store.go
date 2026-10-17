package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/foigent/x-insight/internal/investigation"
)

var ErrSessionNotFound = errors.New("authoritative session not found")

func (s *EvidenceStore) ListAuthoritativeSessions(
	ctx context.Context,
	limit int,
) ([]investigation.AuthoritativeSession, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("evidence store is not initialized")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("session limit must be between 1 and 100")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT session.id, session.source_system_id::text, source.source_type,
       source.display_name, session.source_session_ref, session.source_session_type,
       session.observed_at, count(link.execution_id), session.metadata::text
FROM authoritative_sessions session
JOIN source_systems source ON source.id = session.source_system_id
LEFT JOIN session_execution_links link ON link.session_id = session.id
GROUP BY session.id, source.source_type, source.display_name
ORDER BY session.observed_at DESC, session.id
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]investigation.AuthoritativeSession, 0)
	for rows.Next() {
		item, err := scanAuthoritativeSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *EvidenceStore) GetAuthoritativeSession(
	ctx context.Context,
	sessionID string,
) (investigation.AuthoritativeSessionDetail, error) {
	if s == nil || s.db == nil {
		return investigation.AuthoritativeSessionDetail{}, fmt.Errorf("evidence store is not initialized")
	}
	item, err := scanAuthoritativeSession(s.db.QueryRowContext(ctx, `
SELECT session.id, session.source_system_id::text, source.source_type,
       source.display_name, session.source_session_ref, session.source_session_type,
       session.observed_at, count(link.execution_id), session.metadata::text
FROM authoritative_sessions session
JOIN source_systems source ON source.id = session.source_system_id
LEFT JOIN session_execution_links link ON link.session_id = session.id
WHERE session.id = $1
GROUP BY session.id, source.source_type, source.display_name`, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return investigation.AuthoritativeSessionDetail{}, ErrSessionNotFound
	}
	if err != nil {
		return investigation.AuthoritativeSessionDetail{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT execution_id, source_link_ref, linked_at
FROM session_execution_links
WHERE session_id = $1
ORDER BY linked_at, execution_id`, sessionID)
	if err != nil {
		return investigation.AuthoritativeSessionDetail{}, err
	}
	defer rows.Close()
	links := make([]investigation.SessionExecutionLink, 0)
	for rows.Next() {
		var link investigation.SessionExecutionLink
		if err := rows.Scan(&link.ExecutionID, &link.SourceLinkRef, &link.LinkedAt); err != nil {
			return investigation.AuthoritativeSessionDetail{}, err
		}
		link.LinkedAt = link.LinkedAt.UTC()
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return investigation.AuthoritativeSessionDetail{}, err
	}
	return investigation.AuthoritativeSessionDetail{Session: item, Executions: links}, nil
}

func scanAuthoritativeSession(scanner rowScanner) (investigation.AuthoritativeSession, error) {
	var item investigation.AuthoritativeSession
	var metadata []byte
	if err := scanner.Scan(
		&item.SessionID,
		&item.SourceSystemID,
		&item.SourceType,
		&item.SourceDisplayName,
		&item.SourceSessionRef,
		&item.SourceSessionType,
		&item.ObservedAt,
		&item.ExecutionCount,
		&metadata,
	); err != nil {
		return investigation.AuthoritativeSession{}, err
	}
	item.ObservedAt = item.ObservedAt.UTC()
	if err := json.Unmarshal(metadata, &item.Metadata); err != nil {
		return investigation.AuthoritativeSession{}, err
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}
