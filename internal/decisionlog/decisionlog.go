// Package decisionlog writes and reads decision-log entries (FR-17): every
// relationship verdict, routing decision, LLM verdict and ingest round leaves
// one human-readable line with a structured payload in decision_log.
//
// M1-07 adds relationship entries; routing (M1-08), ingest (M1-06) and LLM
// (M1-11) entries plug into the same Log.
package decisionlog

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Store is the slice of *store.Store the log needs.
type Store interface {
	AppendLog(ctx context.Context, kind store.LogKind, ref, summary string, payload any, at time.Time) (int64, error)
	ListLog(ctx context.Context, f store.LogFilter) ([]store.LogEntry, error)
}

// maxSummaryRunes caps the one-line summary; the full explanation stays on the relationship row.
const maxSummaryRunes = 500

// Log writes and reads decision-log entries.
type Log struct {
	store Store
	now   func() time.Time
}

// New returns a Log over s, stamping entries with the current UTC time.
func New(s Store) *Log { return &Log{store: s, now: func() time.Time { return time.Now().UTC() }} }

// WithClock replaces the clock (tests).
func (l *Log) WithClock(now func() time.Time) *Log {
	l.now = now
	return l
}

// RelationshipPayload is the structured part of a relationship entry. The
// contract diff is not repeated here; it lives on the relationships row.
type RelationshipPayload struct {
	RelationshipID int64                      `json:"relationship_id"`
	RoundID        int64                      `json:"round_id"`
	AID            string                     `json:"a_id"`
	BID            string                     `json:"b_id"`
	Type           canonical.RelationshipType `json:"type"`
	Confidence     decimal.Decimal            `json:"confidence"`
	Method         canonical.Method           `json:"method"`
	Vetoes         []canonical.Veto           `json:"vetoes"`
	FactsUsed      []canonical.FactUse        `json:"facts_used"`
	Explanation    string                     `json:"explanation"`
}

// RelationshipRef is the decision_log.ref for a stored relationship.
func RelationshipRef(relationshipID int64) string { return strconv.FormatInt(relationshipID, 10) }

// Relationship logs a stored relationship verdict (one entry per verdict) and
// returns the entry ID. r must already have a row ID and must validate.
func (l *Log) Relationship(ctx context.Context, r canonical.Relationship) (int64, error) {
	if r.ID <= 0 {
		return 0, errors.New("logging relationship: the relationship has not been stored (no id)")
	}
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("logging relationship: %w", err)
	}
	payload := RelationshipPayload{
		RelationshipID: r.ID, RoundID: r.RoundID, AID: r.AID, BID: r.BID, Type: r.Type, Confidence: r.Confidence,
		Method: r.Method, Vetoes: nonNil(r.Vetoes), FactsUsed: nonNil(r.FactsUsed), Explanation: r.Explanation,
	}
	summary := truncate(fmt.Sprintf("%s (%s, %s): %s vs %s. %s", r.Type, r.Confidence, r.Method, r.AID, r.BID, r.Explanation), maxSummaryRunes)
	id, err := l.store.AppendLog(ctx, store.LogRelationship, RelationshipRef(r.ID), summary, payload, l.now())
	if err != nil {
		return 0, fmt.Errorf("logging relationship %d: %w", r.ID, err)
	}
	return id, nil
}

// Relationships returns the newest relationship entries, at most limit.
func (l *Log) Relationships(ctx context.Context, limit int) ([]store.LogEntry, error) {
	return l.store.ListLog(ctx, store.LogFilter{Kind: store.LogRelationship, Limit: limit})
}

// ForRelationship returns the entries about one stored relationship, newest first.
func (l *Log) ForRelationship(ctx context.Context, relationshipID int64) ([]store.LogEntry, error) {
	return l.store.ListLog(ctx, store.LogFilter{Kind: store.LogRelationship, Ref: RelationshipRef(relationshipID)})
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// truncate shortens s to n runes, marking the cut.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	runes := []rune(strings.TrimSpace(s))
	return string(runes[:n-1]) + "…"
}
