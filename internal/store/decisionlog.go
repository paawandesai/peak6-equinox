package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// LogKind is the kind of a decision-log entry.
type LogKind string

// Decision-log kinds.
const (
	LogRelationship LogKind = "relationship"
	LogRouting      LogKind = "routing"
	LogLLM          LogKind = "llm"
	LogIngest       LogKind = "ingest"
)

// Valid reports whether k is a known kind.
func (k LogKind) Valid() bool {
	return k == LogRelationship || k == LogRouting || k == LogLLM || k == LogIngest
}

// LogEntry is a decision_log row. Payload is raw JSON (an object).
type LogEntry struct {
	ID        int64
	Kind      LogKind
	Ref       string // what the entry is about, e.g. a relationship or round ID
	Summary   string // one human-readable line
	Payload   json.RawMessage
	CreatedAt time.Time
}

// AppendLog writes an entry and returns its ID. payload is marshalled to JSON;
// nil stores {}.
func (s *Store) AppendLog(ctx context.Context, kind LogKind, ref, summary string, payload any, at time.Time) (int64, error) {
	if !kind.Valid() {
		return 0, fmt.Errorf("appending decision log: kind %q is not valid", kind)
	}
	if strings.TrimSpace(summary) == "" {
		return 0, errors.New("appending decision log: summary is required")
	}
	body := []byte("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("appending decision log: encoding payload: %w", err)
		}
		body = b
	}
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO decision_log (kind, ref, summary, payload, created_at) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		kind, ref, summary, body, at.UTC()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("appending decision log: %w", err)
	}
	return id, nil
}

const logCols = `id, kind, ref, summary, payload, created_at`

func scanLog(row pgx.Row) (LogEntry, error) {
	var (
		e       LogEntry
		payload []byte
	)
	if err := row.Scan(&e.ID, &e.Kind, &e.Ref, &e.Summary, &payload, &e.CreatedAt); err != nil {
		return LogEntry{}, err
	}
	e.Payload, e.CreatedAt = json.RawMessage(payload), e.CreatedAt.UTC()
	return e, nil
}

// GetLog returns one decision-log entry by ID.
func (s *Store) GetLog(ctx context.Context, id int64) (LogEntry, error) {
	e, err := scanLog(s.pool.QueryRow(ctx, `SELECT `+logCols+` FROM decision_log WHERE id = $1`, id))
	if err != nil {
		return LogEntry{}, notFound(err, fmt.Sprintf("getting decision log %d", id))
	}
	return e, nil
}

// LogFilter narrows ListLog. Zero fields match everything.
type LogFilter struct {
	Kind  LogKind
	Ref   string
	Limit int // default 100, max 10000
}

// ListLog returns entries matching f, newest first.
func (s *Store) ListLog(ctx context.Context, f LogFilter) ([]LogEntry, error) {
	if unstorable(string(f.Kind), f.Ref) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+logCols+` FROM decision_log
		WHERE ($1 = '' OR kind = $1) AND ($2 = '' OR ref = $2)
		ORDER BY created_at DESC, id DESC LIMIT $3`,
		string(f.Kind), f.Ref, clampLimit(f.Limit, defaultListLimit, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("listing decision log: %w", err)
	}
	defer rows.Close()
	var out []LogEntry
	for rows.Next() {
		e, err := scanLog(rows)
		if err != nil {
			return nil, fmt.Errorf("listing decision log: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing decision log: %w", err)
	}
	return out, nil
}
